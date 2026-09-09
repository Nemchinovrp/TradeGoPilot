package orderflow

import (
	"testing"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func book(t time.Time, bidQty, askQty int64) *pb.OrderBook {
	return &pb.OrderBook{Time: timestamppb.New(t), IsConsistent: true,
		Bids: []*pb.Order{{Price: &pb.Quotation{Units: 300}, Quantity: bidQty}},
		Asks: []*pb.Order{{Price: &pb.Quotation{Units: 300, Nano: 10_000_000}, Quantity: askQty}}}
}

func feed(a *Analyzer, start time.Time, bid, ask int64, direction pb.TradeDirection) {
	for i := 0; i <= 12; i++ {
		now := start.Add(time.Duration(i) * time.Second)
		a.Book(book(now, bid, ask), now)
		a.Trade(&pb.Trade{Time: timestamppb.New(now), Quantity: 10, Direction: direction}, now)
	}
}

func TestPressureAndGates(t *testing.T) {
	start := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		bid, ask int64
		trade    pb.TradeDirection
		want     string
	}{
		{"buyers", 900, 100, pb.TradeDirection_TRADE_DIRECTION_BUY, "вверх"},
		{"sellers", 100, 900, pb.TradeDirection_TRADE_DIRECTION_SELL, "вниз"},
		{"contradictory tape", 900, 100, pb.TradeDirection_TRADE_DIRECTION_SELL, "неопределённо"},
		{"balanced book", 500, 500, pb.TradeDirection_TRADE_DIRECTION_BUY, "неопределённо"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a Analyzer
			feed(&a, start, tc.bid, tc.ask, tc.trade)
			s := a.Signal(start.Add(12 * time.Second))
			if !s.Ready || s.Direction != tc.want {
				t.Fatalf("unexpected signal: %+v", s)
			}
			if s.Score < -1 || s.Score > 1 {
				t.Fatalf("score out of range: %v", s.Score)
			}
			if s := a.Signal(start.Add(16 * time.Second)); s.Ready || s.Direction != "неопределённо" {
				t.Fatalf("stale data generated signal: %+v", s)
			}
		})
	}
}

func TestInvalidBookAndWarmup(t *testing.T) {
	now := time.Now()
	for _, mutate := range []func(*pb.OrderBook){
		func(b *pb.OrderBook) { b.IsConsistent = false },
		func(b *pb.OrderBook) { b.Bids = nil },
		func(b *pb.OrderBook) { b.Asks[0].Price.Units = 299 },
		func(b *pb.OrderBook) { b.Bids[0].Quantity = 0 },
		func(b *pb.OrderBook) { b.Time = timestamppb.New(now.Add(time.Second)) },
		func(b *pb.OrderBook) { b.Bids = append(b.Bids, b.Bids[0]) },
	} {
		var a Analyzer
		b := book(now, 900, 100)
		mutate(b)
		if a.Book(b, now) || a.Signal(now).Ready {
			t.Fatal("invalid book accepted")
		}
	}
	var a Analyzer
	a.Book(book(now, 900, 100), now)
	if a.Signal(now).Ready {
		t.Fatal("no warmup")
	}
	// A duplicate frame cannot create artificial order flow or flip the latest book.
	if a.Book(book(now, 100, 900), now) {
		t.Fatal("duplicate timestamp accepted")
	}
	if a.Signal(now).BookImbalance <= 0 {
		t.Fatal("duplicate changed book")
	}
}

func TestBestQueueFlowAndBoundedWindow(t *testing.T) {
	start := time.Now().Truncate(time.Second)
	var a Analyzer
	a.Book(book(start, 100, 100), start)
	next := start.Add(time.Second)
	a.Book(book(next, 200, 50), next)
	if a.Signal(next).OrderFlow <= 0 {
		t.Fatal("bid additions and ask removals should be positive")
	}
	for i := 0; i < 10000; i++ {
		a.Trade(&pb.Trade{Time: timestamppb.New(next), Quantity: 1, Direction: pb.TradeDirection_TRADE_DIRECTION_BUY}, next)
	}
	if len(a.buckets) > 2 {
		t.Fatal("window grows per event")
	}
	// Unknown direction must not be silently counted as selling.
	a.Trade(&pb.Trade{Time: timestamppb.New(next), Quantity: 100000}, next)
	if a.Signal(next).TradeImbalance != 1 {
		t.Fatal("unknown trade affected balance")
	}
	a.Signal(next.Add(20 * time.Second))
	a.prune(next.Add(20 * time.Second))
	if len(a.buckets) != 0 {
		t.Fatal("expired history retained")
	}
}
