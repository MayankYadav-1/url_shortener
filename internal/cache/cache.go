package cache

import (
    "context"
    "time"

    "github.com/redis/go-redis/v9"
)

type Cache struct {
    rdb *redis.Client
    ttl time.Duration
}

func New(rdb *redis.Client, ttl time.Duration) *Cache {
    return &Cache{rdb: rdb, ttl: ttl}
}

func (c *Cache) Get(ctx context.Context, code string) (string, bool) {
    value, err := c.rdb.Get(ctx, "url:"+code).Result()
    if err != nil {
        return "", false
    }
    return value, true
}

func (c *Cache) Set(ctx context.Context, code, url string) {
    c.SetWithTTL(ctx, code, url, c.ttl)
}

func (c *Cache) SetWithTTL(ctx context.Context, code, url string, ttl time.Duration) {
    if ttl <= 0 {
        return
    }
    _ = c.rdb.Set(ctx, "url:"+code, url, ttl).Err()
}

func (c *Cache) Delete(ctx context.Context, code string) {
    _ = c.rdb.Del(ctx, "url:"+code).Err()
}
