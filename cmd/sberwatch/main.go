package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"tradegopilot/internal/dashboard"
	"tradegopilot/internal/invest"
	"tradegopilot/internal/orderflow"
)

type event struct {
	response *pb.MarketDataResponse
	err      error
	fatal    bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func receive(ctx context.Context, client *invest.Client, uid string, events chan<- event) {
	defer close(events)
	delay := time.Second
	for ctx.Err() == nil {
		err := client.WatchMarket(ctx, uid, func(r *pb.MarketDataResponse) error {
			delay = time.Second
			select {
			case events <- event{response: r}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if ctx.Err() != nil {
			return
		}
		fatal := false
		switch status.Code(err) {
		case codes.Unauthenticated, codes.PermissionDenied, codes.InvalidArgument, codes.Unimplemented:
			fatal = true
		}
		select {
		case events <- event{err: err, fatal: fatal}:
		case <-ctx.Done():
			return
		}
		if fatal {
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func run() (retErr error) {
	duration := flag.Duration("duration", 0, "время наблюдения, например 2m; 0 — до Ctrl+C")
	interval := flag.Duration("interval", 5*time.Second, "интервал сигналов, минимум 1s")
	uiAddress := flag.String("ui", "127.0.0.1:5498", "локальный адрес веб-интерфейса; пустая строка отключает UI")
	flag.Parse()
	if *duration < 0 || *interval < time.Second || flag.NArg() != 0 {
		return errors.New("duration должен быть >=0, interval >=1s; позиционные аргументы не поддерживаются")
	}
	cfg, err := invest.ConfigFromEnv()
	if err != nil {
		return err
	}
	hub := dashboard.New(cfg.Environment, *interval)
	var uiErrors <-chan error
	if *uiAddress != "" {
		ui, err := dashboard.Start(*uiAddress, hub)
		if err != nil {
			return err
		}
		defer func() { retErr = errors.Join(retErr, ui.Close()) }()
		uiErrors = ui.Errors
		fmt.Println("Веб-интерфейс:", ui.URL)
	}
	client, err := invest.New(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *duration > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}
	instrument, err := client.Instrument(ctx, "SBER", "TQBR")
	if err != nil {
		return err
	}
	runID := time.Now().UTC().Format("20060102T150405.000000000Z")
	fmt.Printf("SBER (%s), %s. Наблюдение без заявок. Данные только в памяти.\n", instrument.Name, cfg.Environment)
	fmt.Println("Ожидание подтверждения подписок на стакан и сделки…")
	var analyzer orderflow.Analyzer
	var evaluator orderflow.Evaluator
	publishResults := func(results []orderflow.Evaluation) {
		for _, r := range results {
			hub.Evaluation(r)
		}
	}
	defer func() {
		publishResults(evaluator.Reset(time.Now(), "наблюдение завершено"))
	}()
	streamCtx, cancel := context.WithCancel(ctx)
	events := make(chan event, 256)
	go receive(streamCtx, client, instrument.Uid, events)
	defer func() {
		cancel()
		for range events {
		}
	}()
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	connected := false
	var sequence int
	for {
		select {
		case err := <-uiErrors:
			if err != nil {
				return fmt.Errorf("веб-интерфейс: %w", err)
			}
			return errors.New("веб-интерфейс остановлен")
		case <-ctx.Done():
			fmt.Println("Наблюдение завершено.")
			return nil
		case e, ok := <-events:
			if !ok {
				if ctx.Err() != nil {
					return nil
				}
				return errors.New("поток данных завершился")
			}
			now := time.Now()
			if e.err != nil {
				hub.Connection("reconnecting", "Связь с API потеряна. Переподключаемся…")
				publishResults(evaluator.Reset(now, "разрыв потока"))
				analyzer = orderflow.Analyzer{}
				connected = false
				if e.fatal {
					return e.err
				}
				fmt.Fprintln(os.Stderr, "Переподключение:", e.err)
				continue
			}
			if !connected {
				hub.Connection("connected", "")
				fmt.Println("Обе подписки подтверждены. Накапливаю данные…")
				connected = true
			}
			r := e.response
			if b := r.GetOrderbook(); b != nil && b.InstrumentUid == instrument.Uid {
				hub.Book(b, now)
				if analyzer.Book(b, now) {
					publishResults(evaluator.Observe(now, b.Time.AsTime(), orderflow.Mid(b)))
				}
			}
			if t := r.GetTrade(); t != nil && t.InstrumentUid == instrument.Uid {
				hub.Trade(t)
				analyzer.Trade(t, now)
			}
		case <-ticker.C:
			now := time.Now()
			publishResults(evaluator.Observe(now, time.Time{}, 0))
			s := analyzer.Signal(now)
			sequence++
			s.ID = fmt.Sprintf("%s-%d", runID, sequence)
			evaluator.Add(s)
			hub.Signal(s)
			fmt.Printf("%s SBER: %s | оценка %+.0f/100 | mid %.2f ₽ | %s\n", now.Format("15:04:05"), s.Direction, s.Score*100, s.Mid, s.Reason)
		}
	}
}
