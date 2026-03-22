package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"

	"github.com/redis/go-redis/v9"

	"github.com/aaayushm23/context-engine/pkg/models"
)

type AnalyticsConsumer struct {
	client *redis.Client
	logger *slog.Logger

	// Simple in-memory counters (in production, use Prometheus)
	TotalRecommendations atomic.Int64
	LLMCount             atomic.Int64
	RulesCount           atomic.Int64
	TotalLatencyMs       atomic.Int64
}

func NewAnalyticsConsumer(client *redis.Client, logger *slog.Logger) *AnalyticsConsumer {
	return &AnalyticsConsumer{client: client, logger: logger}
}

func (ac *AnalyticsConsumer) Start(ctx context.Context) {
	sub := ac.client.Subscribe(ctx, "recommendation.created")
	ch := sub.Channel()

	go func() {
		for {
			select {
			case <-ctx.Done():
				sub.Close()
				return
			case msg := <-ch:
				var event models.RecommendationEvent
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					ac.logger.Error("failed to unmarshal event", "error", err)
					continue
				}
				ac.processEvent(event)
			}
		}
	}()

	ac.logger.Info("analytics consumer started")
}

func (ac *AnalyticsConsumer) processEvent(event models.RecommendationEvent) {
	ac.TotalRecommendations.Add(1)
	ac.TotalLatencyMs.Add(event.LatencyMs)

	switch event.Source {
	case "llm":
		ac.LLMCount.Add(1)
	case "rules":
		ac.RulesCount.Add(1)
	}

	ac.logger.Info("recommendation tracked",
		"request_id", event.RequestID,
		"source", event.Source,
		"latency_ms", event.LatencyMs,
		"partners", len(event.Payload.Experiences),
	)
}

// Stats returns current analytics for the /metrics endpoint
func (ac *AnalyticsConsumer) Stats() map[string]interface{} {
	total := ac.TotalRecommendations.Load()
	avgLatency := int64(0)
	if total > 0 {
		avgLatency = ac.TotalLatencyMs.Load() / total
	}

	return map[string]interface{}{
		"total_recommendations": total,
		"llm_count":             ac.LLMCount.Load(),
		"rules_fallback_count":  ac.RulesCount.Load(),
		"avg_latency_ms":        avgLatency,
	}
}
