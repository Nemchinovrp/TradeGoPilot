package orderflow

import "time"

type Evaluation struct {
	SignalID  string    `json:"signal_id"`
	Horizon   int       `json:"horizon_seconds"`
	Time      time.Time `json:"time"`
	Status    string    `json:"status"`
	Direction string    `json:"direction"`
	StartMid  float64   `json:"start_mid"`
	EndMid    float64   `json:"end_mid,omitempty"`
	MoveBPS   float64   `json:"move_bps,omitempty"`
	Correct   *bool     `json:"correct,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

type pending struct {
	signal  Signal
	horizon int
}
type Evaluator struct{ pending []pending }

func (e *Evaluator) Add(s Signal) {
	if !s.Ready || s.Direction == "неопределённо" {
		return
	}
	for _, h := range []int{10, 30, 60} {
		e.pending = append(e.pending, pending{s, h})
	}
}

// Observe uses the first fresh book at or after the target exchange timestamp.
// Missing quotes are explicitly recorded, never counted as successful forecasts.
func (e *Evaluator) Observe(now, bookTime time.Time, mid float64) []Evaluation {
	var results []Evaluation
	remaining := e.pending[:0]
	for _, p := range e.pending {
		due := p.signal.Time.Add(time.Duration(p.horizon) * time.Second)
		r := Evaluation{SignalID: p.signal.ID, Horizon: p.horizon, Time: now, Direction: p.signal.Direction, StartMid: p.signal.Mid}
		if mid > 0 && !bookTime.Before(due) && !bookTime.After(due.Add(MaxAge)) && now.Sub(bookTime) >= 0 && now.Sub(bookTime) <= MaxAge {
			r.Status = "measured"
			r.Time = bookTime
			r.EndMid = mid
			r.MoveBPS = (mid - p.signal.Mid) / p.signal.Mid * 10000
			ok := (p.signal.Direction == "вверх" && r.MoveBPS > 0) || (p.signal.Direction == "вниз" && r.MoveBPS < 0)
			r.Correct = &ok
			results = append(results, r)
		} else if now.After(due.Add(MaxAge)) {
			r.Status = "unavailable"
			r.Reason = "нет свежего стакана в пределах 3 секунд после горизонта"
			results = append(results, r)
		} else {
			remaining = append(remaining, p)
		}
	}
	e.pending = remaining
	return results
}

func (e *Evaluator) Reset(now time.Time, reason string) []Evaluation {
	var out []Evaluation
	for _, p := range e.pending {
		out = append(out, Evaluation{SignalID: p.signal.ID, Horizon: p.horizon, Time: now, Status: "unavailable", Direction: p.signal.Direction, StartMid: p.signal.Mid, Reason: reason})
	}
	e.pending = nil
	return out
}
