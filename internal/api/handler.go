package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/aaayushm23/context-engine/internal/cache"
	contextpkg "github.com/aaayushm23/context-engine/internal/context"
	"github.com/aaayushm23/context-engine/internal/engine"
	"github.com/aaayushm23/context-engine/internal/events"
	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/pkg/models"
)

type Handler struct {
	pipeline       *contextpkg.Pipeline
	decisionEngine *engine.DecisionEngine
	partnerRepo    *partner.Repository
	cache          *cache.RedisCache
	publisher      *events.Publisher
	analytics      *events.AnalyticsConsumer
	logger         *slog.Logger
}

func NewHandler(
	pipeline *contextpkg.Pipeline,
	decisionEngine *engine.DecisionEngine,
	partnerRepo *partner.Repository,
	cache *cache.RedisCache,
	publisher *events.Publisher,
	analytics *events.AnalyticsConsumer,
	logger *slog.Logger,
) *Handler {
	return &Handler{
		pipeline:       pipeline,
		decisionEngine: decisionEngine,
		partnerRepo:    partnerRepo,
		cache:          cache,
		publisher:      publisher,
		analytics:      analytics,
		logger:         logger,
	}
}

// POST /v1/recommend
func (h *Handler) HandleRecommend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req models.RecommendationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	// Get or generate request ID
	reqID, _ := r.Context().Value(RequestIDKey).(string)
	if req.RequestID != "" {
		reqID = req.RequestID
	}

	// Check for force-rules header (feature flag for demo/testing)
	forceRules := r.Header.Get("X-Force-Rules") == "true"

	// Build initial context
	rc := &models.RecommendationContext{
		RequestID:      reqID,
		Lat:            req.Lat,
		Lon:            req.Lon,
		AvailableHours: req.AvailableHours,
		Preferences:    req.Preferences,
	}

	// Run enrichment pipeline (parallel, resilient)
	h.pipeline.Run(r.Context(), rc)

	// Generate recommendation (LLM with fallback, or forced rules)
	resp, err := h.decisionEngine.GenerateRecommendation(r.Context(), rc, forceRules)
	if err != nil {
		h.logger.Error("recommendation failed", "error", err, "request_id", reqID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "recommendation failed"})
		return
	}

	// Publish event — use request context so it cancels on shutdown
	go h.publisher.PublishRecommendation(r.Context(), resp)

	writeJSON(w, http.StatusOK, resp)
}

// GET /v1/partners
func (h *Handler) HandleListPartners(w http.ResponseWriter, r *http.Request) {
	partners, err := h.partnerRepo.GetAll(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, partners)
}

// GET /health — actually checks Postgres and Redis connectivity
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	if err := h.partnerRepo.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"reason": "postgres: " + err.Error(),
		})
		return
	}
	if err := h.cache.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"reason": "redis: " + err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "context-engine",
	})
}

// GET /metrics
func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.analytics.Stats())
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
