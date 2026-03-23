package redis

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqme/loka/internal/controlplane/ha"
)

// Cache implements ha.Cache using Redis.
type Cache struct {
	client redis.UniversalClient
	prefix string
}

// NewCache creates a new Redis cache.
func NewCache(client redis.UniversalClient, prefix string) *Cache {
	if prefix == "" {
		prefix = "loka:cache:"
	}
	return &Cache{client: client, prefix: prefix}
}

func (c *Cache) key(k string) string {
	return c.prefix + k
}

func (c *Cache) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := c.client.Get(ctx, c.key(key)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return val, true, nil
}

func (c *Cache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return c.client.Set(ctx, c.key(key), value, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, c.key(key)).Err()
}

var _ ha.Cache = (*Cache)(nil)
