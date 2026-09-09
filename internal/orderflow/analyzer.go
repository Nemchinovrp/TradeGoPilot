// Package orderflow implements an uncalibrated, read-only market pressure heuristic.
package orderflow

import (
	"fmt"
	"math"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
)

const (
	Window = 10 * time.Second
	MaxAge = 3 * time.Second
)

type Signal struct {
	ID             string    `json:"id"`
	Time           time.Time `json:"time"`
	Direction      string    `json:"direction"`
	Score          float64   `json:"score"` // [-1,1], not a probability
	Mid            float64   `json:"mid"`
	SpreadBPS      float64   `json:"spread_bps"`
	BookImbalance  float64   `json:"book_imbalance"`
	OrderFlow      float64   `json:"order_flow"`
	TradeImbalance float64   `json:"trade_imbalance"`
	Trades         int       `json:"trades"`
	Ready          bool      `json:"ready"`
	Reason         string    `json:"reason"`
}

type bucket struct {
	time                   time.Time
	buy, sell, flow, scale float64
	trades, books          int
}

type Analyzer struct {
	book    *pb.OrderBook
	started time.Time
	buckets []bucket
}

func Price(q *pb.Quotation) float64 { return float64(q.GetUnits()) + float64(q.GetNano())/1e9 }

func ValidBook(b *pb.OrderBook, now time.Time) bool {
	if b == nil || b.GetTime() == nil || b.Time.CheckValid() != nil || !b.IsConsistent || len(b.Bids) == 0 || len(b.Asks) == 0 {
		return false
	}
	age := now.Sub(b.Time.AsTime())
	if age < 0 || age > MaxAge {
		return false
	}
	for side, levels := range [][]*pb.Order{b.Bids, b.Asks} {
		for i, l := range levels {
			if Price(l.GetPrice()) <= 0 || l.GetQuantity() <= 0 {
				return false
			}
			if i > 0 {
				p, prev := Price(l.Price), Price(levels[i-1].Price)
				if (side == 0 && p >= prev) || (side == 1 && p <= prev) {
					return false
				}
			}
		}
	}
	return Price(b.Bids[0].Price) < Price(b.Asks[0].Price)
}

func Mid(b *pb.OrderBook) float64 {
	return (Price(b.Bids[0].Price) + Price(b.Asks[0].Price)) / 2
}

func (a *Analyzer) prune(now time.Time) {
	cutoff := now.Add(-Window)
	out := a.buckets[:0]
	for _, b := range a.buckets {
		if !b.time.Before(cutoff) {
			out = append(out, b)
		}
	}
	a.buckets = out
}

// One-second buckets bound memory even during bursts of trades.
func (a *Analyzer) bucket(t, now time.Time) *bucket {
	a.prune(now)
	t = t.Truncate(time.Second)
	for i := range a.buckets {
		if a.buckets[i].time.Equal(t) {
			return &a.buckets[i]
		}
	}
	a.buckets = append(a.buckets, bucket{time: t})
	return &a.buckets[len(a.buckets)-1]
}

// Book resets the warmup on invalid data or a gap. Older/duplicate frames are ignored.
func (a *Analyzer) Book(b *pb.OrderBook, now time.Time) bool {
	if !ValidBook(b, now) {
		*a = Analyzer{}
		return false
	}
	if a.book != nil && !b.Time.AsTime().After(a.book.Time.AsTime()) {
		return false
	}
	if a.book != nil && b.Time.AsTime().Sub(a.book.Time.AsTime()) > MaxAge {
		*a = Analyzer{}
	}
	if a.started.IsZero() {
		a.started = now
	}
	x := a.bucket(b.Time.AsTime(), now)
	x.books++
	if a.book != nil {
		old := a.book
		bid, ask := b.Bids[0], b.Asks[0]
		ob, oa := old.Bids[0], old.Asks[0]
		// Best-queue order-flow imbalance accounts for price moves as well as sizes.
		if Price(bid.Price) >= Price(ob.Price) {
			x.flow += float64(bid.Quantity)
		}
		if Price(bid.Price) <= Price(ob.Price) {
			x.flow -= float64(ob.Quantity)
		}
		if Price(ask.Price) <= Price(oa.Price) {
			x.flow -= float64(ask.Quantity)
		}
		if Price(ask.Price) >= Price(oa.Price) {
			x.flow += float64(oa.Quantity)
		}
		x.scale += float64(bid.Quantity) + float64(ask.Quantity) + float64(ob.Quantity) + float64(oa.Quantity)
	}
	a.book = b
	return true
}

func (a *Analyzer) Trade(t *pb.Trade, now time.Time) {
	if t.GetTime() == nil || t.Time.CheckValid() != nil || t.Quantity <= 0 {
		return
	}
	age := now.Sub(t.Time.AsTime())
	if age < 0 || age > Window {
		return
	}
	if t.Direction != pb.TradeDirection_TRADE_DIRECTION_BUY && t.Direction != pb.TradeDirection_TRADE_DIRECTION_SELL {
		return
	}
	x := a.bucket(t.Time.AsTime(), now)
	x.trades++
	if t.Direction == pb.TradeDirection_TRADE_DIRECTION_BUY {
		x.buy += float64(t.Quantity)
	} else {
		x.sell += float64(t.Quantity)
	}
}

func (a *Analyzer) Signal(now time.Time) Signal {
	s := Signal{Time: now, Direction: "неопределённо", Reason: "нет свежего согласованного стакана"}
	if !ValidBook(a.book, now) {
		return s
	}
	s.Mid = Mid(a.book)
	s.SpreadBPS = (Price(a.book.Asks[0].Price) - Price(a.book.Bids[0].Price)) / s.Mid * 10000
	var bids, asks float64
	for i, l := range a.book.Bids {
		bids += float64(l.Quantity) / float64(i+1)
	}
	for i, l := range a.book.Asks {
		asks += float64(l.Quantity) / float64(i+1)
	}
	s.BookImbalance = (bids - asks) / (bids + asks)
	a.prune(now)
	var buy, sell, flow, scale float64
	var books int
	for _, x := range a.buckets {
		buy += x.buy
		sell += x.sell
		flow += x.flow
		scale += x.scale
		s.Trades += x.trades
		books += x.books
	}
	if buy+sell > 0 {
		s.TradeImbalance = (buy - sell) / (buy + sell)
	}
	if scale > 0 {
		s.OrderFlow = math.Max(-1, math.Min(1, 2*flow/scale))
	}
	s.Score = .45*s.BookImbalance + .30*s.OrderFlow + .25*s.TradeImbalance
	s.Reason = "прогрев: нужны 10 секунд наблюдения, 3 стакана и 3 сделки в окне"
	if now.Sub(a.started) < Window || books < 3 || s.Trades < 3 {
		return s
	}
	if s.SpreadBPS > 10 {
		s.Reason = "слишком широкий спред (>10 б.п.)"
		return s
	}
	s.Ready = true
	if s.Score >= .25 && s.BookImbalance > .1 && s.TradeImbalance > .1 && s.OrderFlow >= 0 {
		s.Direction = "вверх"
	}
	if s.Score <= -.25 && s.BookImbalance < -.1 && s.TradeImbalance < -.1 && s.OrderFlow <= 0 {
		s.Direction = "вниз"
	}
	s.Reason = fmt.Sprintf("стакан %+.2f; поток заявок %+.2f; сделки %+.2f (%d); спред %.2f б.п.", s.BookImbalance, s.OrderFlow, s.TradeImbalance, s.Trades, s.SpreadBPS)
	return s
}
