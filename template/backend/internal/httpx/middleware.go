package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type ctxKey int

const requestIDKey ctxKey = iota

// RequestID assigns (or propagates X-Request-ID) a request id and echoes it.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" || len(id) > 64 {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// Logger emits one structured line per request (skips health checks).
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // hijacked (websocket) or no explicit write
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		}
		slog.Log(r.Context(), level, "http",
			"method", r.Method,
			"path", logPath(r.URL.Path),
			"status", status,
			"bytes", ww.BytesWritten(),
			"dur_ms", time.Since(start).Milliseconds(),
			"request_id", RequestIDFrom(r.Context()),
			"ip", ClientIPFrom(r),
		)
	})
}

// redactedPrefixes are paths whose last segment is a credential rather than
// an id (push tokens, calendar feed tokens): request logs show the route,
// not the value. Modules add theirs with RedactPathPrefix in New.
var (
	redactMu         sync.RWMutex
	redactedPrefixes = []string{"/v1/devices/"}
)

// RedactPathPrefix hides whatever follows prefix in request logs, e.g.
// RedactPathPrefix("/v1/events/feed/") for secret calendar URLs.
func RedactPathPrefix(prefixes ...string) {
	redactMu.Lock()
	redactedPrefixes = append(redactedPrefixes, prefixes...)
	redactMu.Unlock()
}

func logPath(p string) string {
	redactMu.RLock()
	defer redactMu.RUnlock()
	for _, prefix := range redactedPrefixes {
		if strings.HasPrefix(p, prefix) && len(p) > len(prefix) {
			return prefix + ":redacted"
		}
	}
	return p
}

// Detach shields a handler from client cancellation: the request context
// keeps its values (identity, request id) but is no longer canceled when the
// client disconnects or the router's request timeout fires; it gets its own
// deadline instead. Mount it on routes that move money or otherwise talk to a
// payment provider inside a transaction, so a dropped connection can't roll
// back the database after the provider already acted (and a retry can't then
// act twice).
func Detach(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Recoverer turns panics into 500s with a logged stack.
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.Error("panic", "err", rec, "path", r.URL.Path, "request_id", RequestIDFrom(r.Context()),
					"stack", string(debug.Stack()))
				JSON(w, http.StatusInternalServerError, map[string]any{
					"error": &Error{Code: "internal", Message: "Something went wrong. Please try again."},
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets conservative headers for a JSON API.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// RateLimitedHandler is the httprate limit handler rendering our envelope.
func RateLimitedHandler(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusTooManyRequests, map[string]any{"error": ErrRateLimited})
}
