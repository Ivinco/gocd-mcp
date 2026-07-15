package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/ivinco/gocd-mcp/internal/auth"
	"github.com/ivinco/gocd-mcp/internal/obs"
)

type ctxKey int

const loginHolderKey ctxKey = iota

// loginHolder is a mutable cell shared down the request chain so that the access
// log (which sits above authentication) can record the login that the bearer-token
// middleware resolves further down. Without it, the principal — added to a child
// context below — would not be visible to the access-log middleware.
type loginHolder struct{ login string }

// statusRecorder captures the response status code for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// recoverMW converts panics into 500s without leaking the stack to the client.
func recoverMW(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.ErrorContext(r.Context(), "panic recovered", "panic", v, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestIDMW attaches a random request ID to the context for correlation.
func requestIDMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := obs.NewRequestID()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(obs.WithRequestID(r.Context(), id)))
	})
}

// accessLogMW logs one line per request. It never logs headers or tokens. The
// authenticated login is included when captureLoginMW (below the auth middleware)
// has resolved it for this request.
func accessLogMW(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		holder := &loginHolder{}
		r = r.WithContext(context.WithValue(r.Context(), loginHolderKey, holder))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if id := obs.RequestIDFromContext(r.Context()); id != "" {
			attrs = append(attrs, "request_id", id)
		}
		if holder.login != "" {
			attrs = append(attrs, "login", holder.login)
		}
		log.InfoContext(r.Context(), "request", attrs...)
	})
}

// captureLoginMW records the authenticated login into the request's loginHolder so
// the access log can report it. It must be mounted below the bearer-token middleware.
func captureLoginMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := auth.PrincipalFromContext(r.Context()); ok {
			if h, ok := r.Context().Value(loginHolderKey).(*loginHolder); ok {
				h.login = p.Login
			}
		}
		next.ServeHTTP(w, r)
	})
}
