package orderflow

import (
	"testing"
	"time"
)

func TestEvaluationHorizons(t *testing.T) {
	now := time.Now()
	var e Evaluator
	e.Add(Signal{ID: "s1", Time: now, Ready: true, Direction: "вверх", Mid: 100})
	if len(e.Observe(now.Add(9*time.Second), now.Add(9*time.Second), 101)) != 0 {
		t.Fatal("evaluated before horizon")
	}
	r := e.Observe(now.Add(10*time.Second), now.Add(10*time.Second), 101)
	if len(r) != 1 || r[0].Horizon != 10 || r[0].Correct == nil || !*r[0].Correct || r[0].MoveBPS != 100 {
		t.Fatalf("bad evaluation: %+v", r)
	}
	r = e.Observe(now.Add(30*time.Second), now.Add(30*time.Second), 100)
	if len(r) != 1 || r[0].Correct == nil || *r[0].Correct {
		t.Fatal("unchanged price counted as success")
	}
	r = e.Observe(now.Add(64*time.Second), now.Add(64*time.Second), 102)
	if len(r) != 1 || r[0].Status != "unavailable" || r[0].Correct != nil {
		t.Fatal("late quote counted as success")
	}
	if len(e.pending) != 0 {
		t.Fatal("evaluated signals retained")
	}
}

func TestEvaluationSkipsNeutralAndResets(t *testing.T) {
	var e Evaluator
	now := time.Now()
	e.Add(Signal{Ready: true, Direction: "неопределённо"})
	e.Add(Signal{Direction: "вверх"})
	if len(e.pending) != 0 {
		t.Fatal("non-directional signal evaluated")
	}
	e.Add(Signal{ID: "s1", Time: now, Ready: true, Direction: "вниз", Mid: 100})
	if r := e.Observe(now.Add(10*time.Second), now.Add(9*time.Second), 99); len(r) != 0 {
		t.Fatal("used pre-horizon quote")
	}
	r := e.Reset(now, "disconnect")
	if len(r) != 3 || r[0].Status != "unavailable" || len(e.pending) != 0 {
		t.Fatal("reset lost unmeasured results")
	}
}
