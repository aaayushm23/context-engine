package context

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aaayushm23/context-engine/internal/cache"
	"github.com/aaayushm23/context-engine/internal/resilience"
	"github.com/aaayushm23/context-engine/pkg/models"
)

// Enricher defines a uniform boundary for all external data integrations.
// This interface allows the pipeline to dynamically scale or swap signals without refactoring.
type Enricher interface {
	Enrich(ctx context.Context, rc *models.RecommendationContext) error
	Name() string
}

// Pipeline serves as the orchestration layer for context assimilation.
// By centralizing timeout budgets and circuit breakers here, we isolate the HTTP layer
// from the complexities of partial graph resolution and concurrent remote calls.
type Pipeline struct {
	enrichers []Enricher
	breakers  map[string]*resilience.CircuitBreaker
	cache     *cache.RedisCache
	budget    *resilience.BudgetManager
	logger    *slog.Logger
}

func NewPipeline(
	enrichers []Enricher,
	cache *cache.RedisCache,
	budget *resilience.BudgetManager,
	logger *slog.Logger,
) *Pipeline {
	breakers := make(map[string]*resilience.CircuitBreaker)
	for _, e := range enrichers {
		breakers[e.Name()] = resilience.NewCircuitBreaker(
			e.Name(), 3, 30*time.Second,
		)
	}
	return &Pipeline{
		enrichers: enrichers,
		breakers:  breakers,
		cache:     cache,
		budget:    budget,
		logger:    logger,
	}
}

// Run coordinates concurrent execution across all registered enrichers.
// It enforces scatter-gather semantics where stragglers are ignored once the
// parent context timeout expires, prioritizing consistent latency over data completeness.
func (p *Pipeline) Run(ctx context.Context, rc *models.RecommendationContext) {
	stageCtx, cancel := p.budget.StageContext(ctx, "context_fetch")
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, enricher := range p.enrichers {
		wg.Add(1)
		go func(e Enricher) {
			defer wg.Done()

			cb := p.breakers[e.Name()]
			err := cb.Execute(func() error {
				return e.Enrich(stageCtx, rc)
			})

			mu.Lock()
			defer mu.Unlock()

			signal := models.ContextSignal(e.Name())
			if err != nil {
				p.logger.Warn("enricher failed",
					"enricher", e.Name(),
					"request_id", rc.RequestID,
					"error", err,
				)
				rc.SignalsFailed = append(rc.SignalsFailed, signal)
			} else {
				rc.SignalsUsed = append(rc.SignalsUsed, signal)
			}
		}(enricher)
	}

	wg.Wait()

	// Build semantic tags from collected context — APPEND to existing, don't override
	p.buildSemanticTags(rc)
}

// buildSemanticTags bridges the gap between disparate contextual signals and the unified
// prompt ontology required by the LLM by synthesizing high-level behavioral tags.
func (p *Pipeline) buildSemanticTags(rc *models.RecommendationContext) {
	var inferred []string

	// Weather-based tags
	switch rc.WeatherTag {
	case "indoor_weather":
		inferred = append(inferred, "indoor", "rainy_day", "covered")
	case "outdoor_weather":
		inferred = append(inferred, "outdoor", "open_air", "nature")
	}

	// Time-based tags
	switch rc.TimeSlot {
	case "weekend_afternoon", "weekend_morning":
		inferred = append(inferred, "weekend", "leisure")
	case "weekday_evening":
		inferred = append(inferred, "after_work", "evening")
	}

	// Append user preferences (already expanded by PreferenceEnricher)
	inferred = append(inferred, rc.Preferences...)

	// We explicitly append rather than overwrite to preserve signals
	// that were explicitly injected by the client request or upstream processes.
	rc.SemanticTags = append(rc.SemanticTags, inferred...)
}
