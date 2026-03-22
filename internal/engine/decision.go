package engine

import (
	"context"
	"log/slog"
	"time"

	"github.com/aaayushm23/context-engine/internal/cache"
	"github.com/aaayushm23/context-engine/internal/llm"
	"github.com/aaayushm23/context-engine/internal/partner"
	"github.com/aaayushm23/context-engine/internal/resilience"
	"github.com/aaayushm23/context-engine/pkg/models"
)

type DecisionEngine struct {
	llmClient   *llm.OllamaClient
	partnerRepo *partner.Repository
	cache       *cache.RedisCache
	budget      *resilience.BudgetManager
	llmBreaker  *resilience.CircuitBreaker
	logger      *slog.Logger
}

func NewDecisionEngine(
	llmClient *llm.OllamaClient,
	partnerRepo *partner.Repository,
	cache *cache.RedisCache,
	budget *resilience.BudgetManager,
	logger *slog.Logger,
) *DecisionEngine {
	return &DecisionEngine{
		llmClient:   llmClient,
		partnerRepo: partnerRepo,
		cache:       cache,
		budget:      budget,
		llmBreaker:  resilience.NewCircuitBreaker("llm", 3, 30*time.Second),
		logger:      logger,
	}
}

func (de *DecisionEngine) GenerateRecommendation(
	ctx context.Context,
	rc *models.RecommendationContext,
) (*models.RecommendationResponse, error) {
	start := time.Now()

	// 1. Check idempotency — return cached result if same request
	idemKey := "idem:" + rc.RequestID
	var cachedResp models.RecommendationResponse
	exists, err := de.cache.CheckIdempotency(ctx, idemKey)
	if err == nil && exists {
		de.cache.Get(ctx, idemKey, &cachedResp)
		cachedResp.Meta.FromCache = true
		de.logger.Info("idempotent hit", "request_id", rc.RequestID)
		return &cachedResp, nil
	}

	// 2. Check LLM response cache (content-hash based)
	cacheKey := cache.ContentHash(struct {
		Tags     []string
		City     string
		Weather  string
		TimeSlot string
	}{rc.SemanticTags, rc.City, rc.WeatherCondition, rc.TimeSlot})

	var cachedRec models.Recommendation
	if hit, _ := de.cache.Get(ctx, cacheKey, &cachedRec); hit {
		de.logger.Info("LLM cache hit", "request_id", rc.RequestID)
		resp := de.buildResponse(rc, cachedRec, time.Since(start), []string{"llm_response"})
		return resp, nil
	}

	// 3. Match partners from Postgres
	matchCtx, matchCancel := de.budget.StageContext(ctx, "partner_match")
	defer matchCancel()

	matchedPartners, err := de.partnerRepo.MatchByTags(matchCtx, rc.SemanticTags, rc.Lat, rc.Lon, 10)
	if err != nil {
		de.logger.Error("partner match failed", "error", err, "request_id", rc.RequestID)
		matchedPartners = []partner.Partner{} // continue with empty
	}

	// 4. Try LLM, fall back to rules
	var recommendation models.Recommendation
	var cacheHits []string

	llmCtx, llmCancel := de.budget.StageContext(ctx, "llm_call")
	defer llmCancel()

	var llmResult *llm.LLMResult
	llmErr := de.llmBreaker.Execute(func() error {
		var innerErr error
		llmResult, innerErr = de.llmClient.ComposeRecommendation(llmCtx, rc, matchedPartners)
		return innerErr
	})

	if llmErr != nil {
		// Graceful degradation: fall back to rules
		de.logger.Warn("LLM failed, using rule-based fallback",
			"error", llmErr,
			"request_id", rc.RequestID,
		)
		recommendation = RuleBasedFallback(rc, matchedPartners)
	} else {
		// Build recommendation from LLM result
		recommendation = buildFromLLM(rc, llmResult, matchedPartners)

		// Cache the LLM response
		de.cache.Set(ctx, cacheKey, recommendation, 30*time.Minute)
	}

	// 5. Build final response
	resp := de.buildResponse(rc, recommendation, time.Since(start), cacheHits)

	// 6. Store for idempotency
	de.cache.SetIdempotency(ctx, idemKey, resp, 5*time.Minute)

	return resp, nil
}

func (de *DecisionEngine) buildResponse(
	rc *models.RecommendationContext,
	rec models.Recommendation,
	latency time.Duration,
	cacheHits []string,
) *models.RecommendationResponse {
	return &models.RecommendationResponse{
		RequestID:      rc.RequestID,
		Recommendation: rec,
		Meta: models.ResponseMeta{
			LatencyMs: latency.Milliseconds(),
			CacheHits: cacheHits,
			FromCache: false,
		},
	}
}

func buildFromLLM(rc *models.RecommendationContext, result *llm.LLMResult, partners []partner.Partner) models.Recommendation {
	// Map LLM partner names back to actual partner data
	partnerMap := make(map[string]partner.Partner)
	for _, p := range partners {
		partnerMap[p.Name] = p
	}

	var experiences []models.Experience
	for _, exp := range result.Experiences {
		e := models.Experience{
			PartnerName: exp.PartnerName,
			Category:    exp.Category,
			Reason:      exp.Reason,
		}
		if p, ok := partnerMap[exp.PartnerName]; ok {
			e.PartnerID = p.ID
			e.DistanceKm = partner.HaversineDistance(rc.Lat, rc.Lon, p.Lat, p.Lon)
		}
		experiences = append(experiences, e)
	}

	signalsUsed := make([]string, len(rc.SignalsUsed))
	for i, s := range rc.SignalsUsed {
		signalsUsed[i] = string(s)
	}
	signalsFailed := make([]string, len(rc.SignalsFailed))
	for i, s := range rc.SignalsFailed {
		signalsFailed[i] = string(s)
	}

	return models.Recommendation{
		Title:                result.Title,
		Experiences:          experiences,
		TotalDurationHrs:     rc.AvailableHours,
		Source:               "llm",
		ContextSignalsUsed:   signalsUsed,
		ContextSignalsFailed: signalsFailed,
	}
}
