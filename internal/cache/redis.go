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

// Get retrieves a cached value and unmarshals it into dest
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

// Set stores a value with TTL
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// CheckIdempotency returns true if this request was already processed
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

// SetIdempotency marks a request as processed
func (c *RedisCache) SetIdempotency(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return c.Set(ctx, key, value, ttl)
}

// AcquireIdempotencyLock attempts to acquire a processing lock for a request_id.
// Uses SETNX (SetNX) — only succeeds if the key doesn't exist.
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

// ReleaseIdempotencyLock releases the processing lock
func (c *RedisCache) ReleaseIdempotencyLock(ctx context.Context, requestID string) {
	lockKey := "lock:" + requestID
	c.client.Del(ctx, lockKey)
}

// ContentHash generates a cache key from context data
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
