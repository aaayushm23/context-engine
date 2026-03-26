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
	forceRules bool,
) (*models.RecommendationResponse, error) {
	start := time.Now()
	idemKey := "idem:" + rc.RequestID

	// ── Phase 1: Edge Idempotency ──
	// Intercepting at the very beginning of the pipeline prevents expensive graph resolution
	// and LLM invocations for client-side retries caused by network drops.
	var cachedResp models.RecommendationResponse
	exists, err := de.cache.CheckIdempotency(ctx, idemKey)
	if err == nil && exists {
		de.cache.Get(ctx, idemKey, &cachedResp)
		cachedResp.Meta.FromCache = true
		de.logger.Info("idempotent hit", "request_id", rc.RequestID)
		return &cachedResp, nil
	}

	// ── Phase 2: Distributed Concurrency Control ──
	// Thundering herds are mitigated here. If a client sends three identical requests instantly,
	// only one worker is allowed to proceed; the others sleep and wait for the cache to populate.
	acquired, lockErr := de.cache.AcquireIdempotencyLock(ctx, rc.RequestID, 30*time.Second)
	if lockErr != nil {
		de.logger.Warn("failed to acquire idempotency lock", "error", lockErr, "request_id", rc.RequestID)
	} else if !acquired {
		time.Sleep(100 * time.Millisecond)
		if exists, _ := de.cache.CheckIdempotency(ctx, idemKey); exists {
			de.cache.Get(ctx, idemKey, &cachedResp)
			cachedResp.Meta.FromCache = true
			return &cachedResp, nil
		}
		de.logger.Info("lock held by another request, proceeding", "request_id", rc.RequestID)
	} else {
		defer de.cache.ReleaseIdempotencyLock(ctx, rc.RequestID)
	}

	// ── Phase 3: Deterministic Candidate Generation ──
	// We mandate that all partner matching uses the fast, rule-based engine. This guarantees
	// spatial and semantic constraints are respected before handing data to the non-deterministic LLM.
	matchCtx, matchCancel := de.budget.StageContext(ctx, "partner_match")
	candidates, err := de.partnerRepo.MatchByTags(matchCtx, rc.SemanticTags, rc.Lat, rc.Lon, 10)
	matchCancel() // Canceling the scoped context immediately frees up timing budget for downstream phases.

	if err != nil {
		de.logger.Error("partner match failed", "error", err, "request_id", rc.RequestID)
		candidates = []partner.Partner{}
	}

	// ── Phase 4: Probabilistic Reranking (LLM) ──
	// The LLM acts purely as a presentation and selection layer over the pre-filtered candidates.
	// This separation of concerns prevents the LLM from hallucinating non-existent inventory.
	var recommendation models.Recommendation

	if !forceRules && len(candidates) > 0 {
		cacheKey := cache.ContentHash(struct {
			Tags     []string
			City     string
			Weather  string
			TimeSlot string
		}{rc.SemanticTags, rc.City, rc.WeatherCondition, rc.TimeSlot})

		var cachedRec models.Recommendation
		if hit, _ := de.cache.Get(ctx, cacheKey, &cachedRec); hit {
			de.logger.Info("LLM rerank cache hit", "request_id", rc.RequestID)
			recommendation = cachedRec
		} else {
			llmCandidates := toLLMCandidates(rc, candidates)

			llmCtx, llmCancel := de.budget.StageContext(ctx, "llm_call")

			var rerankResult *llm.RerankResult
			var llmErr error

			llmErr = de.llmBreaker.Execute(func() error {
				var innerErr error
				rerankResult, innerErr = de.llmClient.RerankCandidates(llmCtx, rc, llmCandidates)
				return innerErr
			})
			llmCancel() // Strict resource management: release the context the millisecond the LLM IO completes.

			if llmErr != nil {
				de.logger.Warn("LLM reranking failed, using rule-based selection",
					"error", llmErr,
					"request_id", rc.RequestID,
				)
				recommendation = RuleBasedFallback(rc, candidates)
			} else {
				recommendation = buildFromRerank(rc, rerankResult, candidates)
				de.cache.Set(ctx, cacheKey, recommendation, 30*time.Minute)
			}
		}
	} else {
		if forceRules {
			de.logger.Info("forced rules mode", "request_id", rc.RequestID)
		}
		recommendation = RuleBasedFallback(rc, candidates)
	}

	// ── Phase 5: Result Commit ──
	// Committing the final response guarantees subsequent requests within the TTL retrieve
	// this exact payload from Phase 1 without executing the pipeline.
	resp := &models.RecommendationResponse{
		RequestID:      rc.RequestID,
		Recommendation: recommendation,
		Meta: models.ResponseMeta{
			LatencyMs: time.Since(start).Milliseconds(),
			FromCache: false,
		},
	}

	de.cache.SetIdempotency(ctx, idemKey, resp, 5*time.Minute)

	return resp, nil
}

func toLLMCandidates(rc *models.RecommendationContext, partners []partner.Partner) []llm.Candidate {
	candidates := make([]llm.Candidate, len(partners))
	for i, p := range partners {
		candidates[i] = llm.Candidate{
			Name:       p.Name,
			Category:   p.Category,
			Tags:       p.SemanticTags,
			DistanceKm: partner.HaversineDistance(rc.Lat, rc.Lon, p.Lat, p.Lon),
		}
	}
	return candidates
}

func buildFromRerank(rc *models.RecommendationContext, result *llm.RerankResult, candidates []partner.Partner) models.Recommendation {
	partnerMap := make(map[string]partner.Partner)
	for _, p := range candidates {
		partnerMap[p.Name] = p
	}

	var experiences []models.Experience
	for _, sel := range result.Selections {
		e := models.Experience{
			PartnerName: sel.PartnerName,
			Category:    sel.Role,
			Reason:      sel.Reason,
		}
		if p, ok := partnerMap[sel.PartnerName]; ok {
			e.PartnerID = p.ID
			e.Category = p.Category
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
