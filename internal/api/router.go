package api

import (
	"log/slog"
	"net/http"
	"time"
)

func NewRouter(h *Handler, logger *slog.Logger, requestTimeout time.Duration) http.Handler {
	mux := http.NewServeMux()

	// Versioned API endpoints
	mux.HandleFunc("/v1/recommend", h.HandleRecommend)
	mux.HandleFunc("/v1/partners", h.HandleListPartners)

	// System endpoints
	mux.HandleFunc("/health", h.HandleHealth)
	mux.HandleFunc("/metrics", h.HandleMetrics)

	// Apply middleware chain (innermost first)
	var handler http.Handler = mux
	handler = TimeoutMiddleware(requestTimeout)(handler)
	handler = LoggingMiddleware(logger)(handler)
	handler = RequestIDMiddleware(handler)
	handler = RecoveryMiddleware(logger)(handler)

	return handler
}
