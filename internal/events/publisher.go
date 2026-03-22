package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/aaayushm23/context-engine/pkg/models"
)

type Publisher struct {
	client *redis.Client
	logger *slog.Logger
}

func NewPublisher(client *redis.Client, logger *slog.Logger) *Publisher {
	return &Publisher{client: client, logger: logger}
}

func (p *Publisher) PublishRecommendation(ctx context.Context, resp *models.RecommendationResponse) {
	event := models.RecommendationEvent{
		RequestID: resp.RequestID,
		EventType: "recommendation.created",
		Payload:   resp.Recommendation,
		LatencyMs: resp.Meta.LatencyMs,
		Source:    resp.Recommendation.Source,
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		p.logger.Error("failed to marshal event", "error", err)
		return
	}

	if err := p.client.Publish(ctx, "recommendation.created", data).Err(); err != nil {
		p.logger.Error("failed to publish event", "error", err)
	}
}
