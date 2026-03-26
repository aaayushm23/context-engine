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

	// Thread-safe accumulators prevent read/write races during concurrent event ingestion.
	// Keeping them unexported forces structural encapsulation via the Stats() accessor.
	totalRecommendations atomic.Int64
	llmCount             atomic.Int64
	rulesCount           atomic.Int64
	totalLatencyMs       atomic.Int64
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
	ac.totalRecommendations.Add(1)
	ac.totalLatencyMs.Add(event.LatencyMs)

	switch event.Source {
	case "llm":
		ac.llmCount.Add(1)
	case "rules":
		ac.rulesCount.Add(1)
	}

	ac.logger.Info("recommendation tracked",
		"request_id", event.RequestID,
		"source", event.Source,
		"latency_ms", event.LatencyMs,
		"partners", len(event.Payload.Experiences),
	)
}

// Stats surfaces atomic point-in-time metrics without halting the event loop,
// enabling zero-latency health and throughput monitoring.
func (ac *AnalyticsConsumer) Stats() map[string]interface{} {
	total := ac.totalRecommendations.Load()
	avgLatency := int64(0)
	if total > 0 {
		avgLatency = ac.totalLatencyMs.Load() / total
	}

	return map[string]interface{}{
		"total_recommendations": total,
		"llm_count":             ac.llmCount.Load(),
		"rules_fallback_count":  ac.rulesCount.Load(),
		"avg_latency_ms":        avgLatency,
	}
}
