package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisStore struct {
	Client   *redis.Client
	Sliding  time.Duration
	Absolute time.Duration
	Prefix   string
}

func NewRedisStore(client *redis.Client, sliding, absolute time.Duration) *RedisStore {
	return &RedisStore{Client: client, Sliding: sliding, Absolute: absolute, Prefix: "connect:session:"}
}

func (s *RedisStore) Create(ctx context.Context, subject string, now time.Time) (Session, error) {
	if s == nil || s.Client == nil || subject == "" || s.Sliding <= 0 || s.Absolute <= 0 {
		return Session{}, errors.New("Redis session store and positive TTLs are required")
	}
	id, err := randomSessionID()
	if err != nil {
		return Session{}, err
	}
	now = now.UTC()
	session := Session{
		ID: id, UserSubject: subject, CreatedAt: now, LastSeenAt: now,
		ExpiresAt:         expiry(now, s.Sliding, s.Absolute),
		AbsoluteExpiresAt: now.Add(s.Absolute),
	}
	if err := s.save(ctx, session, now); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *RedisStore) Get(ctx context.Context, id string, now time.Time) (Session, error) {
	if s == nil || s.Client == nil {
		return Session{}, errors.New("Redis session store is not initialized")
	}
	raw, err := s.Client.Get(ctx, redisKey(s.Prefix, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, err
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return Session{}, errors.New("invalid session record")
	}
	if !validAt(session, now) {
		_ = s.Client.Del(ctx, redisKey(s.Prefix, id)).Err()
		return Session{}, ErrNotFound
	}
	session.ID = id
	return session, nil
}

func (s *RedisStore) Touch(ctx context.Context, id string, now time.Time) (Session, error) {
	session, err := s.Get(ctx, id, now)
	if err != nil {
		return Session{}, err
	}
	now = now.UTC()
	session.LastSeenAt = now
	session.ExpiresAt = expiry(now, s.Sliding, session.AbsoluteExpiresAt.Sub(now))
	if session.ExpiresAt.After(session.AbsoluteExpiresAt) {
		session.ExpiresAt = session.AbsoluteExpiresAt
	}
	if err := s.save(ctx, session, now); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *RedisStore) Delete(ctx context.Context, id string) error {
	if s == nil || s.Client == nil {
		return errors.New("Redis session store is not initialized")
	}
	err := s.Client.Del(ctx, redisKey(s.Prefix, id)).Err()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	return err
}

func (s *RedisStore) save(ctx context.Context, session Session, now time.Time) error {
	ttl := session.ExpiresAt.Sub(now.UTC())
	if ttl <= 0 {
		return ErrNotFound
	}
	raw, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.Client.Set(ctx, redisKey(s.Prefix, session.ID), raw, ttl).Err()
}

func redisKey(prefix, id string) string {
	sum := sha256.Sum256([]byte(id))
	return prefix + hex.EncodeToString(sum[:])
}

func randomSessionID() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
