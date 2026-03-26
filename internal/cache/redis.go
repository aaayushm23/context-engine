package cache

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(addr string) *RedisCache {
	return &RedisCache{
		client: redis.NewClient(&redis.Options{
			Addr: addr,
		}),
	}
}

// Get encapsulates the underlying Redis protocol, insulating domain logic from byte-level
// unmarshaling and standardizing the handling of expected misses versus actual connection errors.
func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) (bool, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil // cache miss
	}
	if err != nil {
		return false, err
	}

	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return false, err
	}
	return true, nil
}

// Set enforces a mandatory TTL argument. This prevents unbounded memory growth
// and aligns with our design philosophy that cache data is inherently ephemeral.
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// CheckIdempotency guards against duplicate processing (e.g., from client retries).
// By verifying history at the edge, we protect expensive downstream ML inference endpoints.
func (c *RedisCache) CheckIdempotency(ctx context.Context, key string) (bool, error) {
	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return val != "", nil
}

// SetIdempotency commits the processed outcome, ensuring future identical requests
// bypass the engine and serve the cached result immediately.
func (c *RedisCache) SetIdempotency(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return c.Set(ctx, key, value, ttl)
}

// AcquireIdempotencyLock provides distributed concurrency control. If two identical requests
// hit different pods at the exact same moment, SETNX ensures only one initiates the heavy
// context enrichment and LLM generation loop.
// Returns true if lock was acquired (this request should proceed).
// Returns false if another request is already processing this ID.
func (c *RedisCache) AcquireIdempotencyLock(ctx context.Context, requestID string, ttl time.Duration) (bool, error) {
	lockKey := "lock:" + requestID
	ok, err := c.client.SetNX(ctx, lockKey, "processing", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("acquire idempotency lock: %w", err)
	}
	return ok, nil
}

// ReleaseIdempotencyLock allows subsequent retries to proceed if the initial worker
// crashed or failed before setting the final idempotency record.
func (c *RedisCache) ReleaseIdempotencyLock(ctx context.Context, requestID string) {
	lockKey := "lock:" + requestID
	c.client.Del(ctx, lockKey)
}

// ContentHash provides a deterministic cache key derived purely from payload semantics,
// allowing cache hits even when the HTTP-level request IDs differ but the query is identical.
func ContentHash(data interface{}) string {
	bytes, _ := json.Marshal(data)
	hash := sha256.Sum256(bytes)
	return fmt.Sprintf("cache:llm:%x", hash[:8])
}

// Ping checks Redis connectivity
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Client exposes the raw Redis client for Pub/Sub usage
func (c *RedisCache) Client() *redis.Client {
	return c.client
}
