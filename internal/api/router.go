package api

import (
	"log/slog"
	"net/http"
	"time"
)

func NewRouter(h *Handler, logger *slog.Logger, requestTimeout time.Duration) http.Handler {
	mux := http.NewServeMux()

	// We explicitly version API routes at the multiplexer level to allow
// parallel staging of breaking backend contract changes without affecting v1 clients.
	mux.HandleFunc("/v1/recommend", h.HandleRecommend)
	mux.HandleFunc("/v1/partners", h.HandleListPartners)

	// System endpoints are kept unversioned and typically exposed on a separate internal port
// in production to isolate operational traffic from public ingestion.
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/metrics", h.HandleMetrics)

	// The middleware chain is constructed concentrically (innermost executes last).
// This guarantees that critical safeguards like panics (Recovery) are caught at the outermost envelope,
// while metrics (Logging) accurately measure the full execution time.
	var handler http.Handler = mux
	handler = TimeoutMiddleware(requestTimeout)(handler)
	handler = LoggingMiddleware(logger)(handler)
	handler = RequestIDMiddleware(handler)
	handler = RecoveryMiddleware(logger)(handler)

	return handler
}
