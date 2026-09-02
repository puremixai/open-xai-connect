package identity

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultLevelProgressCachePrefix = "connect:identity:level-progress:v1:"

var errLevelProgressCacheMiss = errors.New("level progress cache miss")

type levelProgressCacheBackend interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string, time.Duration) error
	Delete(context.Context, string) error
}

type RedisLevelProgressCache struct {
	backend levelProgressCacheBackend
	prefix  string
	ttl     time.Duration
}

func NewRedisLevelProgressCache(client *redis.Client, prefix string, ttl time.Duration) *RedisLevelProgressCache {
	if strings.TrimSpace(prefix) == "" {
		prefix = defaultLevelProgressCachePrefix
	}
	return &RedisLevelProgressCache{
		backend: redisLevelProgressCacheBackend{client: client},
		prefix:  prefix,
		ttl:     ttl,
	}
}

func (c *RedisLevelProgressCache) Get(ctx context.Context, discourseID int64) (LevelProgressSnapshot, bool, error) {
	if c == nil || c.backend == nil {
		return LevelProgressSnapshot{}, false, errors.New("Redis level progress cache is not initialized")
	}
	encoded, err := c.backend.Get(ctx, c.key(discourseID))
	if err != nil {
		if errors.Is(err, errLevelProgressCacheMiss) {
			return LevelProgressSnapshot{}, false, nil
		}
		return LevelProgressSnapshot{}, false, err
	}
	var snapshot LevelProgressSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return LevelProgressSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (c *RedisLevelProgressCache) Set(ctx context.Context, snapshot LevelProgressSnapshot, ttl time.Duration) error {
	if c == nil || c.backend == nil {
		return errors.New("Redis level progress cache is not initialized")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return c.backend.Set(ctx, c.key(snapshot.DiscourseID), string(encoded), ttl)
}

func (c *RedisLevelProgressCache) Invalidate(ctx context.Context, discourseID int64) error {
	if c == nil || c.backend == nil {
		return errors.New("Redis level progress cache is not initialized")
	}
	return c.backend.Delete(ctx, c.key(discourseID))
}

func (c *RedisLevelProgressCache) key(discourseID int64) string {
	return c.prefix + strconvFormatInt(discourseID)
}

type redisLevelProgressCacheBackend struct {
	client *redis.Client
}

func (b redisLevelProgressCacheBackend) Get(ctx context.Context, key string) (string, error) {
	if b.client == nil {
		return "", errors.New("Redis level progress cache is not initialized")
	}
	value, err := b.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", errLevelProgressCacheMiss
	}
	return value, err
}

func (b redisLevelProgressCacheBackend) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if b.client == nil {
		return errors.New("Redis level progress cache is not initialized")
	}
	return b.client.Set(ctx, key, value, ttl).Err()
}

func (b redisLevelProgressCacheBackend) Delete(ctx context.Context, key string) error {
	if b.client == nil {
		return errors.New("Redis level progress cache is not initialized")
	}
	return b.client.Del(ctx, key).Err()
}

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
