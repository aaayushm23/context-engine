package context

import (
	"context"
	"log/slog"
	"sync"

	"github.com/aaayushm23/context-engine/internal/cache"
	"github.com/aaayushm23/context-engine/internal/resilience"
	"github.com/aaayushm23/context-engine/pkg/models"
)

// Enricher adds context signals to the recommendation context
type Enricher interface {
	Enrich(ctx context.Context, rc *models.RecommendationContext) error
	Name() string
}

// Pipeline runs all enrichers in parallel with resilience
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
			e.Name(), 3, 30*1000*1000*1000, // 30 seconds
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

// Run executes all enrichers in parallel, collecting results
// Failed enrichers are recorded but don't stop the pipeline
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

	// Build semantic tags from collected context
	p.buildSemanticTags(rc)
}

// buildSemanticTags derives matching tags from enriched context
func (p *Pipeline) buildSemanticTags(rc *models.RecommendationContext) {
	tags := []string{}

	// Weather-based tags
	switch rc.WeatherTag {
	case "indoor_weather":
		tags = append(tags, "indoor", "rainy_day", "covered")
	case "outdoor_weather":
		tags = append(tags, "outdoor", "open_air", "nature")
	}

	// Time-based tags
	switch rc.TimeSlot {
	case "weekend_afternoon", "weekend_morning":
		tags = append(tags, "weekend", "leisure")
	case "weekday_evening":
		tags = append(tags, "after_work", "evening")
	}

	// Preference-based tags
	tags = append(tags, rc.Preferences...)

	rc.SemanticTags = tags
}
