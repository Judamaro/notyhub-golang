package api

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// RequestRecorder records HTTP server metrics; RequestStarted returns the function that records the finished request.
type RequestRecorder interface {
	RequestStarted() func(route, method string, status int, duration time.Duration)
}

// statusWriter captures the status code and keeps the optional interfaces (hijacking for WebSocket upgrades,
// flushing) of the wrapped writer.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *statusWriter) Flush() { _ = http.NewResponseController(w.ResponseWriter).Flush() }

// petitionWriter replaces the plain-text error bodies written by the standard library (unknown route, method
// not allowed, failed upgrade, ...) with a PetitionResponse of module Api.
type petitionWriter struct {
	statusWriter
	intercepted bool
}

func (w *petitionWriter) WriteHeader(code int) {
	if w.status == 0 && code >= http.StatusBadRequest && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.intercepted = true
		w.Header().Del("Content-Length")
		w.Header().Del("X-Content-Type-Options")
		w.status = code
		writeStatus(&w.statusWriter, code)
		return
	}
	w.statusWriter.WriteHeader(code)
}

func (w *petitionWriter) Write(b []byte) (int, error) {
	if w.intercepted {
		return len(b), nil
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.statusWriter.Write(b)
}

// withPetitionStatusPages keeps every error response in the PetitionResponse format.
func withPetitionStatusPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&petitionWriter{statusWriter: statusWriter{ResponseWriter: w}}, r)
	})
}

// withRecover turns panics into a 500 PetitionResponse, hiding internal details from clients.
func withRecover(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
					panic(v)
				}
				logger.ErrorContext(r.Context(), "Unhandled panic processing request",
					"method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				Fail(ModuleGlobalExceptionHandler, "An unexpected error occurred.", http.StatusInternalServerError, nil).write(w)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// withCORS allows the configured browser origins, with credentials, any header and any method.
func withCORS(allowedOrigins []string) Middleware {
	allowed := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		allowed[strings.TrimSuffix(o, "/")] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || !allowed[origin] {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")

			if method := r.Header.Get("Access-Control-Request-Method"); r.Method == http.MethodOptions && method != "" {
				h.Set("Access-Control-Allow-Methods", method)
				if headers := r.Header.Get("Access-Control-Request-Headers"); headers != "" {
					h.Set("Access-Control-Allow-Headers", headers)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// withHTTPMetrics records the duration of each request by route pattern, method and status. Requests whose path
// is in excluded (health checks) are not recorded.
func withHTTPMetrics(recorder RequestRecorder, excluded ...string) Middleware {
	return func(next http.Handler) http.Handler {
		if recorder == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, path := range excluded {
				if r.URL.Path == path {
					next.ServeHTTP(w, r)
					return
				}
			}

			start := time.Now()
			done := recorder.RequestStarted()
			sw := &statusWriter{ResponseWriter: w}
			defer func() {
				route := r.Pattern
				if route == "" {
					route = "unmatched"
				} else if _, path, ok := strings.Cut(route, " "); ok {
					route = path
				}
				status := sw.status
				if status == 0 {
					status = http.StatusOK
				}
				done(route, r.Method, status, time.Since(start))
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

// chain applies the middlewares so the first one is the outermost.
func chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
