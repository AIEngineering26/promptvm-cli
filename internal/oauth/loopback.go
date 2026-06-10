package oauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Callback represents the data delivered to the loopback redirect URI.
// Exactly one of (Code, State) or Error will be populated.
type Callback struct {
	Code  string
	State string
	Error string
}

// successHTML is rendered to the user's browser after a successful callback.
// Kept intentionally tiny so it renders offline.
const successHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>PromptVM CLI — Authorized</title>
<style>
  html,body{height:100%;margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;background:#0b0d12;color:#e6e6e6}
  .wrap{display:flex;align-items:center;justify-content:center;height:100%}
  .card{max-width:420px;padding:2rem 2.25rem;border:1px solid #1f2430;border-radius:12px;background:#12151d;text-align:center}
  h1{margin:0 0 .5rem;font-size:1.25rem}
  p{margin:0;color:#9aa3b2;font-size:.95rem}
  .check{font-size:2rem;color:#4ade80;margin-bottom:.25rem}
</style>
</head>
<body>
  <div class="wrap"><div class="card">
    <div class="check">✓</div>
    <h1>You're signed in.</h1>
    <p>You can return to your terminal.</p>
  </div></div>
</body>
</html>`

const failureHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>PromptVM CLI — Error</title></head>
<body style="font-family:system-ui;padding:2rem">
<h1 style="color:#e11d48">Authorization failed</h1>
<p>%s</p>
<p>You can close this tab and try again in your terminal.</p>
</body></html>`

// StartLoopbackServer binds an HTTP server on 127.0.0.1 at a random port
// and returns the port, a channel that will receive exactly one Callback,
// and a shutdown func the caller must invoke when finished.
//
// The server only listens on 127.0.0.1 (not 0.0.0.0 / localhost) per the
// loopback interface redirection guidance in RFC 8252.
//
// Only /callback is handled; all other paths return 404.
func StartLoopbackServer(ctx context.Context) (int, <-chan Callback, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, nil, fmt.Errorf("binding loopback listener: %w", err)
	}
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		ln.Close()
		return 0, nil, nil, fmt.Errorf("unexpected listener address type %T", ln.Addr())
	}
	port := addr.Port

	ch := make(chan Callback, 1)
	var delivered bool

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// Only accept the first /callback hit; subsequent probes get 404'd.
		if delivered {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()

		cb := Callback{
			Code:  q.Get("code"),
			State: q.Get("state"),
			Error: q.Get("error"),
		}
		if desc := q.Get("error_description"); desc != "" && cb.Error != "" {
			cb.Error = cb.Error + ": " + desc
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if cb.Error != "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, failureHTML, cb.Error)
		} else if cb.Code == "" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, failureHTML, "missing authorization code")
			cb.Error = "missing authorization code"
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, successHTML)
		}

		delivered = true
		// Non-blocking send: channel has buffer of 1.
		select {
		case ch <- cb:
		default:
		}
	})
	// Everything else 404s so probes can't grab anything interesting.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		_ = srv.Serve(ln)
	}()

	shutdown := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}

	// If the parent context is cancelled, tear the server down.
	go func() {
		<-ctx.Done()
		shutdown()
	}()

	return port, ch, shutdown, nil
}
