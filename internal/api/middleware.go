package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type contextKey string

const RequestIDKey contextKey = "request_id"

// RequestIDMiddleware ensures traceability across distributed components by injecting a unique ID.
// This is critical for correlating logs and events in out-of-process boundaries (like Redis PubSub)
// and debugging production issues without relying on individual service logs.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get("X-Request-ID")
		if reqID == "" {
			reqID = "req_" + uuid.New().String()[:8]
		}
		ctx := context.WithValue(r.Context(), RequestIDKey, reqID)
		w.Header().Set("X-Request-ID", reqID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware provides a uniform instrumentation point for incoming edge requests.
// By capturing status and duration here, we eliminate the need for handlers to log themselves,
// enforcing a single source of truth for edge monitoring and SLO tracking.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			reqID, _ := r.Context().Value(RequestIDKey).(string)

			// We wrap the ResponseWriter to intercept the written status code,
// allowing the middleware to log the true outcome of a request after the handler completes.
			wrapped := &statusWriter{ResponseWriter: w, status: 200}
			next.ServeHTTP(wrapped, r)

			logger.Info("request completed",
				"request_id", reqID,
				"method", r.Method,
				"path", r.URL.Path,
				"status", wrapped.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// TimeoutMiddleware enforces a strict system-wide SLA to avoid resource exhaustion.
// In high-concurrency systems, unbounded requests tie up goroutines and database connections;
// this context cancellation proactively limits blast radius under severe load or downstream stalls.
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RecoveryMiddleware acts as the last line of defense against nil pointer dereferences
// and other panics that typically crash the entire process. By recovering here,
// we isolate failures to the individual request thread, maintaining overall system availability.
func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered", "error", err)
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
