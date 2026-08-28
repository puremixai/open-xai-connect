package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisNonceStore provides atomic SETNX replay protection shared by Portal
// instances. Redis errors are returned to callers so security-sensitive flows
// fail closed instead of silently accepting an unverifiable request.
type RedisNonceStore struct {
	Client *redis.Client
	Prefix string
}

func NewRedisNonceStore(client *redis.Client, prefix string) *RedisNonceStore {
	if strings.TrimSpace(prefix) == "" {
		prefix = "connect:nonce:"
	}
	return &RedisNonceStore{Client: client, Prefix: prefix}
}

func (s *RedisNonceStore) CheckAndStore(ctx context.Context, nonce string, expiresAt time.Time) (bool, error) {
	if s == nil || s.Client == nil || strings.TrimSpace(nonce) == "" {
		return false, errors.New("Redis nonce store is not initialized")
	}
	ttl := time.Until(expiresAt)
	if ttl <= 0 {
		return false, ErrExpiredRequest
	}
	sum := sha256.Sum256([]byte(nonce))
	return s.Client.SetNX(ctx, s.Prefix+hex.EncodeToString(sum[:]), "1", ttl).Result()
}
