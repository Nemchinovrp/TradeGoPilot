// Package dashboard exposes a local, read-only view of the SBER observer.
package dashboard

import (
	"slices"
	"sync"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"tradegopilot/internal/orderflow"
)

type Level struct {
	Price    float64 `json:"price"`
	Quantity int64   `json:"quantity"`
}
type Book struct {
	Time   time.Time `json:"time"`
	Valid  bool      `json:"valid"`
	Bids   []Level   `json:"bids"`
	Asks   []Level   `json:"asks"`
	Mid    float64   `json:"mid"`
	Spread float64   `json:"spread"`
}
type Point struct {
	Time time.Time `json:"time"`
	Mid  float64   `json:"mid"`
}
type Trade struct {
	Time      time.Time `json:"time"`
	Price     float64   `json:"price"`
	Quantity  int64     `json:"quantity"`
	Direction string    `json:"direction"`
}
type Prediction struct {
	Signal  orderflow.Signal         `json:"signal"`
	Results [3]*orderflow.Evaluation `json:"results"`
}
type Stats struct {
	Horizon     int `json:"horizon"`
	Measured    int `json:"measured"`
	Correct     int `json:"correct"`
	Unavailable int `json:"unavailable"`
}
type State struct {
	ServerTime            time.Time         `json:"server_time"`
	Started               time.Time         `json:"started"`
	Environment           string            `json:"environment"`
	SignalIntervalSeconds float64           `json:"signal_interval_seconds"`
	Status                string            `json:"status"`
	Message               string            `json:"message"`
	Book                  *Book             `json:"book"`
	Signal                *orderflow.Signal `json:"signal"`
	Points                []Point           `json:"points"`
	Trades                []Trade           `json:"trades"`
	Predictions           []Prediction      `json:"predictions"`
	Stats                 [3]Stats          `json:"stats"`
	BookCount             int               `json:"book_count"`
	TradeCount            int               `json:"trade_count"`
}

type Hub struct {
	mu    sync.RWMutex
	state State
}

func New(environment string, interval time.Duration) *Hub {
	return &Hub{state: State{Started: time.Now(), Environment: environment, SignalIntervalSeconds: interval.Seconds(), Status: "connecting", Message: "Подключение к данным SBER…", Stats: [3]Stats{{Horizon: 10}, {Horizon: 30}, {Horizon: 60}}}}
}

func (h *Hub) Connection(status, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.Status, h.state.Message = status, message
	if status != "connected" {
		h.state.Signal = nil
		if h.state.Book != nil {
			b := *h.state.Book
			b.Valid = false
			h.state.Book = &b
		}
	}
}

func (h *Hub) Book(b *pb.OrderBook, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.BookCount++
	valid := orderflow.ValidBook(b, now)
	if valid && h.state.Book != nil && b.GetTime() != nil && !b.Time.AsTime().After(h.state.Book.Time) {
		return
	}
	// Keep the last usable quote visible while explicitly marking it stale.
	if !valid && h.state.Book != nil {
		b := *h.state.Book
		b.Valid = false
		h.state.Book = &b
		return
	}
	x := &Book{Valid: valid}
	if b.GetTime() != nil {
		x.Time = b.Time.AsTime()
	}
	if valid {
		for _, l := range b.Bids {
			x.Bids = append(x.Bids, Level{orderflow.Price(l.Price), l.Quantity})
		}
		for _, l := range b.Asks {
			x.Asks = append(x.Asks, Level{orderflow.Price(l.Price), l.Quantity})
		}
		x.Mid = orderflow.Mid(b)
		x.Spread = x.Asks[0].Price - x.Bids[0].Price
		p := Point{x.Time, x.Mid}
		n := len(h.state.Points)
		if n > 0 && h.state.Points[n-1].Time.Truncate(time.Second).Equal(x.Time.Truncate(time.Second)) {
			h.state.Points[n-1] = p
		} else {
			h.state.Points = append(h.state.Points, p)
		}
		h.state.Points = tail(h.state.Points, 900)
	}
	h.state.Book = x
}

func (h *Hub) Trade(t *pb.Trade) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.TradeCount++
	if t.GetTime() == nil || t.Time.CheckValid() != nil || t.Quantity <= 0 {
		return
	}
	direction := "unknown"
	if t.Direction == pb.TradeDirection_TRADE_DIRECTION_BUY {
		direction = "buy"
	}
	if t.Direction == pb.TradeDirection_TRADE_DIRECTION_SELL {
		direction = "sell"
	}
	h.state.Trades = tail(append(h.state.Trades, Trade{t.Time.AsTime(), orderflow.Price(t.Price), t.Quantity, direction}), 40)
}

func (h *Hub) Signal(s orderflow.Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.Signal = &s
	if s.Ready && s.Direction != "неопределённо" {
		h.state.Predictions = tail(append(h.state.Predictions, Prediction{Signal: s}), 80)
	}
}

func (h *Hub) Evaluation(e orderflow.Evaluation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	index := -1
	for i, s := range h.state.Stats {
		if s.Horizon == e.Horizon {
			index = i
		}
	}
	if index < 0 {
		return
	}
	for i := range h.state.Predictions {
		p := &h.state.Predictions[i]
		if p.Signal.ID == e.SignalID {
			if p.Results[index] != nil {
				return
			}
			p.Results[index] = &e
			break
		}
	}
	s := &h.state.Stats[index]
	if e.Status == "measured" {
		s.Measured++
		if e.Correct != nil && *e.Correct {
			s.Correct++
		}
	} else {
		s.Unavailable++
	}
}

func tail[T any](items []T, n int) []T {
	if len(items) > n {
		items = slices.Clone(items[len(items)-n:])
	}
	return items
}

// Snapshot copies mutable slices; the stream writer never holds the data lock.
func (h *Hub) Snapshot(now time.Time) State {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := h.state
	s.ServerTime = now
	s.Points = slices.Clone(s.Points)
	s.Trades = slices.Clone(s.Trades)
	s.Predictions = slices.Clone(s.Predictions)
	if s.Book != nil {
		b := *s.Book
		b.Bids = slices.Clone(b.Bids)
		b.Asks = slices.Clone(b.Asks)
		s.Book = &b
	}
	if s.Signal != nil {
		signal := *s.Signal
		s.Signal = &signal
	}
	return s
}
