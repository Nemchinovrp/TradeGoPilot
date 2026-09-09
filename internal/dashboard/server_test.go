package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLocalHTTPRoutes(t *testing.T) {
	handler := Handler(New("sandbox", 5*time.Second))
	for _, tc := range []struct {
		path, host, origin string
		code               int
	}{
		{"/", "127.0.0.1:5498", "", 200},
		{"/app.js", "localhost:5498", "", 200},
		{"/style.css", "127.0.0.1:5498", "", 200},
		{"/api/state", "127.0.0.1:5498", "http://127.0.0.1:5498", 200},
		{"/api/state", "evil.example:5498", "", 403},
		{"/api/state", "127.0.0.1:5498", "https://evil.example", 403},
		{"/.env", "127.0.0.1:5498", "", 404},
		{"/api/orders", "127.0.0.1:5498", "", 404},
	} {
		t.Run(tc.path+tc.host+tc.origin, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+tc.path, nil)
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatalf("got %d, want %d", w.Code, tc.code)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("live data may be cached")
			}
		})
	}
}

type streamRecorder struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *streamRecorder) Flush() { w.ResponseRecorder.Flush(); w.cancel() }

func TestSSEInitialSnapshotAndCancellation(t *testing.T) {
	h := New("sandbox", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:5498/api/events", nil).WithContext(ctx)
	w := &streamRecorder{httptest.NewRecorder(), cancel}
	Handler(h).ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatal("not an SSE response")
	}
	data := strings.TrimSpace(strings.TrimPrefix(w.Body.String(), "event: state\ndata: "))
	var s State
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		t.Fatal(err)
	}
	if s.Environment != "sandbox" || s.Status != "connecting" {
		t.Fatalf("bad stream snapshot: %+v", s)
	}
	if strings.Contains(data, "INVEST_TOKEN") || strings.Contains(data, "authorization") {
		t.Fatal("credential included in UI data")
	}
}

func TestRejectPublicBind(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:5498", ":5498", "192.168.1.2:5498", "[::]:5498"} {
		if s, err := Start(addr, New("sandbox", time.Second)); err == nil {
			s.Close()
			t.Fatalf("public bind accepted: %s", addr)
		}
	}
}
