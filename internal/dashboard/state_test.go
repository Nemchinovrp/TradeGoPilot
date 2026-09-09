package dashboard

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	pb "github.com/tinkoff/invest-api-go-sdk/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"tradegopilot/internal/orderflow"
)

func testBook(now time.Time) *pb.OrderBook {
	return &pb.OrderBook{Time: timestamppb.New(now), IsConsistent: true, Bids: []*pb.Order{{Price: &pb.Quotation{Units: 300}, Quantity: 50}}, Asks: []*pb.Order{{Price: &pb.Quotation{Units: 300, Nano: 10_000_000}, Quantity: 70}}}
}

func TestSnapshotsAndConnection(t *testing.T) {
	h := New("sandbox", 5*time.Second)
	now := time.Now()
	h.Connection("connected", "")
	h.Book(testBook(now), now)
	h.Signal(orderflow.Signal{ID: "1", Time: now, Ready: true, Direction: "вверх", Mid: 300.005})
	s := h.Snapshot(now)
	if s.Book.Mid != 300.005 || len(s.Points) != 1 || s.Book.Bids[0].Quantity != 50 {
		t.Fatalf("wrong quote: %+v", s.Book)
	}
	s.Book.Bids[0].Quantity = 999
	s.Points[0].Mid = 1
	s.Signal.Direction = "changed"
	if s = h.Snapshot(now); s.Book.Bids[0].Quantity != 50 || s.Points[0].Mid != 300.005 || s.Signal.Direction != "вверх" {
		t.Fatal("snapshot exposed mutable state")
	}
	b := testBook(now)
	b.IsConsistent = false
	h.Book(b, now)
	if h.Snapshot(now).Book.Valid {
		t.Fatal("inconsistent frame left UI live")
	}
	h.Connection("reconnecting", "retry")
	s = h.Snapshot(now)
	if s.Book != nil || s.Signal != nil || len(s.Predictions) != 1 {
		t.Fatal("disconnect must clear live data and retain history")
	}
}

func TestResultsAndBoundedHistory(t *testing.T) {
	h := New("sandbox", time.Second)
	now := time.Now().Truncate(time.Second)
	for i := 0; i < 1000; i++ {
		ts := now.Add(time.Duration(i) * time.Second)
		h.Book(testBook(ts), ts)
		h.Trade(&pb.Trade{Time: timestamppb.New(ts), Quantity: 1, Direction: pb.TradeDirection_TRADE_DIRECTION_BUY})
	}
	h.Signal(orderflow.Signal{ID: "s", Ready: true, Direction: "вверх", Mid: 300})
	ok := true
	e := orderflow.Evaluation{SignalID: "s", Horizon: 10, Status: "measured", Correct: &ok}
	h.Evaluation(e)
	h.Evaluation(e)
	h.Evaluation(orderflow.Evaluation{SignalID: "s", Horizon: 30, Status: "unavailable"})
	s := h.Snapshot(now)
	if len(s.Points) != 900 || len(s.Trades) != 40 {
		t.Fatal("unbounded market history")
	}
	if s.Stats[0].Measured != 1 || s.Stats[0].Correct != 1 || s.Stats[1].Measured != 0 || s.Stats[1].Unavailable != 1 {
		t.Fatalf("incorrect statistics: %+v", s.Stats)
	}
	if s.Predictions[0].Results[0] == nil || s.Predictions[0].Results[1] == nil {
		t.Fatal("missing prediction results")
	}
}

func TestConcurrentObservationAndReaders(t *testing.T) {
	h := New("sandbox", time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				now := time.Now()
				h.Book(testBook(now), now)
				if _, err := json.Marshal(h.Snapshot(now)); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
