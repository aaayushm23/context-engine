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
