package web

import (
	"encoding/json"
	"log/slog"
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
// wrote, so middlewares can observe the response.
type responseRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
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
