package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	rdb    *redis.Client
	limit  int
	window time.Duration
}

func New(rdb *redis.Client, limit int, window time.Duration) *Limiter {
	return &Limiter{rdb: rdb, limit: limit, window: window}
}

// Sliding-window rate limiting using a Redis sorted set.
func (l *Limiter) Allow(ctx context.Context, key string) (bool, error) {
	now := time.Now().UnixNano()
	start := now - l.window.Nanoseconds()
	redisKey := "rate:" + key

	pipe := l.rdb.TxPipeline()
	pipe.ZRemRangeByScore(ctx, redisKey, "0", strconv.FormatInt(start, 10))
	pipe.ZAdd(ctx, redisKey, redis.Z{Score: float64(now), Member: strconv.FormatInt(now, 10)})
	count := pipe.ZCard(ctx, redisKey)
	pipe.Expire(ctx, redisKey, l.window)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}
	return count.Val() <= int64(l.limit), nil
}
