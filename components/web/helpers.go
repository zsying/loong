package web

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// WriteJSON writes v as a JSON response with the given status code and
// the application/json content type.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("web: encode json", "err", err)
	}
}

// ReadJSON decodes the request body into v. Decode errors are returned
// to the caller, which decides the status code.
func ReadJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// Recover returns a middleware that converts a handler panic into a 500
// response (when nothing was written yet) and logs the panic. It
// re-panics http.ErrAbortHandler so the stdlib server still aborts.
func Recover() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &responseRecorder{ResponseWriter: w}
			defer func() {
				if p := recover(); p != nil {
					if p == http.ErrAbortHandler {
						panic(p)
					}
					slog.Error("web: panic", "panic", p, "method", r.Method, "path", r.URL.Path)
					if !rec.wrote {
						http.Error(w, "internal error", http.StatusInternalServerError)
					}
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// RequestLog returns a middleware that logs every request at debug
// level with method, path, status and duration.
func RequestLog() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			slog.Debug("web request", "method", r.Method, "path", r.URL.Path,
				"status", rec.statusCode(), "dur", time.Since(start).Round(time.Microsecond))
		})
	}
}

// responseRecorder records whether and with which status the handler
// wrote, so middlewares can observe the response. It must stay a
// transparent wrapper: Flush and Hijack are forwarded to the
// underlying writer and Unwrap exposes it, so streaming (SSE, chunked
// downloads) and protocol upgrades (WebSocket) keep working through
// Recover and RequestLog — a wrapper that only embeds the interface
// would silently drop those optional behaviours.
type responseRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// The recorder satisfies the optional writer interfaces unconditionally
// so handlers and libraries can assert on them; the calls forward to
// the underlying writer (see Unwrap).
var (
	_ http.Flusher  = (*responseRecorder)(nil)
	_ http.Hijacker = (*responseRecorder)(nil)
)

// Unwrap returns the wrapped writer, so http.ResponseController and
// interface assertions reach the real implementation instead of the
// recorder (Go 1.20+ standard for transparent wrappers).
func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush forwards to the underlying writer when it supports flushing;
// without it every SSE stream would buffer until the handler returns.
func (w *responseRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying writer when it supports hijacking
// (a real server does), so upgrades such as WebSocket still work.
func (w *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("web: hijack not supported by the underlying writer")
	}
	return h.Hijack()
}

func (w *responseRecorder) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseRecorder) Write(b []byte) (int, error) {
	if !w.wrote {
		w.status = http.StatusOK
		w.wrote = true
	}
	return w.ResponseWriter.Write(b)
}

func (w *responseRecorder) statusCode() int {
	if !w.wrote {
		return http.StatusOK
	}
	return w.status
}
