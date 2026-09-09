package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"time"
)

//go:embed web/*
var assets embed.FS

type Server struct {
	URL    string
	Errors <-chan error
	http   *http.Server
	cancel context.CancelFunc
}

// Start deliberately accepts only loopback addresses. The UI has no account token
// or trading endpoint, and is not intended to be a publicly accessible server.
func Start(address string, hub *Hub) (*Server, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("UI: укажите локальный адрес, например 127.0.0.1:8080")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("UI: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &http.Server{Handler: Handler(hub), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	errorsC := make(chan error, 1)
	go func() {
		err := s.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errorsC <- err
		}
		close(errorsC)
	}()
	return &Server{URL: "http://" + listener.Addr().String(), Errors: errorsC, http: s, cancel: cancel}, nil
}

func (s *Server) Close() error {
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

func Handler(hub *Hub) http.Handler {
	files, _ := fs.Sub(assets, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(hub.Snapshot(time.Now()))
	})
	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		controller := http.NewResponseController(w)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			data, err := json.Marshal(hub.Snapshot(time.Now()))
			if err != nil {
				return
			}
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err = fmt.Fprintf(w, "event: state\ndata: %s\n\n", data); err != nil {
				return
			}
			if err = controller.Flush(); err != nil {
				return
			}
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
		}
	})
	mux.Handle("GET /", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/app.js", "/style.css":
			http.FileServerFS(files).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'none'")
		// Check Host and Origin to keep other websites from reading localhost data.
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "local access only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" || u.Host != r.Host {
				http.Error(w, "same origin required", http.StatusForbidden)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
