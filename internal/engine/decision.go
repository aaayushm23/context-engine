package engine

import (
	"context"
	"errors"
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

	// ── Step 1: Idempotency check ──
	var cachedResp models.RecommendationResponse
	exists, err := de.cache.CheckIdempotency(ctx, idemKey)
	if err == nil && exists {
		de.cache.Get(ctx, idemKey, &cachedResp)
		cachedResp.Meta.FromCache = true
		de.logger.Info("idempotent hit", "request_id", rc.RequestID)
		return &cachedResp, nil
	}

	// ── Step 2: SETNX lock — prevent duplicate concurrent processing ──
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

	// ── Step 3: CANDIDATE GENERATION (rules engine — fast, deterministic) ──
	// Fetch 8-10 candidates from Postgres, filtered by bounding box + semantic tags.
	// This is the "wide net" — we get more candidates than we need.
	matchCtx, matchCancel := de.budget.StageContext(ctx, "partner_match")
	defer matchCancel()

	candidates, err := de.partnerRepo.MatchByTags(matchCtx, rc.SemanticTags, rc.Lat, rc.Lon, 10)
	if err != nil {
		de.logger.Error("partner match failed", "error", err, "request_id", rc.RequestID)
		candidates = []partner.Partner{}
	}

	// ── Step 4: INTELLIGENT RERANKING (LLM — picks best bundle from candidates) ──
	//
	// Architecture: Candidate Generation → Intelligent Reranking
	// Same pattern as Spotify (candidate gen → ML reranker → final list)
	//
	// What the LLM does that rules CANNOT:
	//   - Vibe matching: casual activities pair with casual food
	//   - Flow reasoning: active → food → relax makes sense for evening
	//   - Combination intelligence: which 3-4 of these 10 form the best BUNDLE
	//
	// If LLM fails: rules engine takes the top 3 by score. Still works, just less intelligent.

	var recommendation models.Recommendation

	if !forceRules && len(candidates) > 0 {
		// Check LLM rerank cache
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
			// Convert partners to LLM candidates (decoupled from partner package)
			llmCandidates := toLLMCandidates(rc, candidates)

			llmCtx, llmCancel := de.budget.StageContext(ctx, "llm_call")
			defer llmCancel()

			var rerankResult *llm.RerankResult
			var llmErr error

			llmErr = de.llmBreaker.Execute(func() error {
				var innerErr error
				rerankResult, innerErr = de.llmClient.RerankCandidates(llmCtx, rc, llmCandidates)
				return innerErr
			})

			if llmErr != nil {
				// Graceful degradation: rules select top 3 by score
				de.logger.Warn("LLM reranking failed, using rule-based selection",
					"error", llmErr,
					"request_id", rc.RequestID,
				)
				recommendation = RuleBasedFallback(rc, candidates)
			} else {
				// Build recommendation from LLM reranked selections
				recommendation = buildFromRerank(rc, rerankResult, candidates)

				// Cache the reranked result
				de.cache.Set(ctx, cacheKey, recommendation, 30*time.Minute)
			}
		}
	} else {
		// Forced rules mode or no candidates
		if forceRules {
			de.logger.Info("forced rules mode", "request_id", rc.RequestID)
		}
		recommendation = RuleBasedFallback(rc, candidates)
	}

	// ── Step 5: Build response + store for idempotency ──
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

// toLLMCandidates converts partner models to LLM-friendly candidates
// to avoid circular imports between engine and partner packages
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

// buildFromRerank maps LLM rerank selections back to full partner data
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

// Kept for backward compatibility — errors trigger fallback
var _ = errors.New
