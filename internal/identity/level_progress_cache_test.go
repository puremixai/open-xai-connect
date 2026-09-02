package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store/memory"
)

func TestLevelProgressRefresherCachesBySubjectMapping(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	for _, user := range []domain.User{
		{Subject: "sub_1", DiscourseID: 42, CreatedAt: now, UpdatedAt: now},
		{Subject: "sub_2", DiscourseID: 99, CreatedAt: now, UpdatedAt: now},
	} {
		if err := users.Upsert(ctx, user); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	}
	provider := &levelProgressProviderSpy{
		snapshots: map[int64]LevelProgressSnapshot{
			42: validLevelProgressSnapshot(),
			99: func() LevelProgressSnapshot {
				snapshot := validLevelProgressSnapshot()
				snapshot.DiscourseID = 99
				snapshot.GeneratedAt = snapshot.GeneratedAt.Add(time.Minute)
				return snapshot
			}(),
		},
	}
	cache := newMemoryLevelProgressCache()
	refresher := NewLevelProgressRefresher(provider, users, cache)

	first, err := refresher.CurrentLevelProgress(ctx, "sub_1")
	if err != nil {
		t.Fatalf("CurrentLevelProgress(first) error = %v", err)
	}
	second, err := refresher.CurrentLevelProgress(ctx, "sub_1")
	if err != nil {
		t.Fatalf("CurrentLevelProgress(second) error = %v", err)
	}
	other, err := refresher.CurrentLevelProgress(ctx, "sub_2")
	if err != nil {
		t.Fatalf("CurrentLevelProgress(other) error = %v", err)
	}

	if first.DiscourseID != 42 || second.DiscourseID != 42 {
		t.Fatalf("cached snapshots = %#v / %#v", first, second)
	}
	if other.DiscourseID != 99 {
		t.Fatalf("other snapshot = %#v", other)
	}
	if provider.callsByID[42] != 1 {
		t.Fatalf("provider calls for 42 = %d, want 1", provider.callsByID[42])
	}
	if provider.callsByID[99] != 1 {
		t.Fatalf("provider calls for 99 = %d, want 1", provider.callsByID[99])
	}
	if cache.setCalls != 2 {
		t.Fatalf("cache set calls = %d, want 2", cache.setCalls)
	}
}

func TestLevelProgressRefresherRejectsMismatchedSnapshotBeforeCaching(t *testing.T) {
	ctx := context.Background()
	users := memory.NewUserRepository()
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	if err := users.Upsert(ctx, domain.User{
		Subject: "sub_1", DiscourseID: 42, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	provider := &levelProgressProviderSpy{
		snapshots: map[int64]LevelProgressSnapshot{
			42: func() LevelProgressSnapshot {
				snapshot := validLevelProgressSnapshot()
				snapshot.DiscourseID = 7
				return snapshot
			}(),
		},
	}
	cache := newMemoryLevelProgressCache()
	refresher := NewLevelProgressRefresher(provider, users, cache)

	_, err := refresher.CurrentLevelProgress(ctx, "sub_1")
	if err == nil || !strings.Contains(err.Error(), "discourse id mismatch") {
		t.Fatalf("CurrentLevelProgress() error = %v", err)
	}
	if provider.callsByID[42] != 1 {
		t.Fatalf("provider calls for 42 = %d, want 1", provider.callsByID[42])
	}
	if cache.setCalls != 0 {
		t.Fatalf("cache set calls = %d, want 0", cache.setCalls)
	}
}

func TestRedisLevelProgressCacheGetSetInvalidate(t *testing.T) {
	ctx := context.Background()
	backend := &levelProgressCacheBackendFake{values: make(map[string]string)}
	cache := NewRedisLevelProgressCache(nil, "", 0)
	cache.backend = backend
	snapshot := validLevelProgressSnapshot()

	missed, ok, err := cache.Get(ctx, 42)
	if err != nil {
		t.Fatalf("Get(miss) error = %v", err)
	}
	if ok || missed.DiscourseID != 0 {
		t.Fatalf("Get(miss) snapshot/ok = %#v/%v", missed, ok)
	}
	if err := cache.Set(ctx, snapshot, 0); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	got, ok, err := cache.Get(ctx, 42)
	if err != nil {
		t.Fatalf("Get(hit) error = %v", err)
	}
	if !ok || got.DiscourseID != 42 {
		t.Fatalf("Get(hit) snapshot/ok = %#v/%v", got, ok)
	}
	if backend.lastSetKey != "connect:identity:level-progress:v1:42" {
		t.Fatalf("last set key = %q", backend.lastSetKey)
	}
	if backend.lastSetTTL != 5*time.Minute {
		t.Fatalf("last set TTL = %v, want %v", backend.lastSetTTL, 5*time.Minute)
	}
	if err := cache.Invalidate(ctx, 42); err != nil {
		t.Fatalf("Invalidate() error = %v", err)
	}
	if backend.lastDeletedKey != "connect:identity:level-progress:v1:42" {
		t.Fatalf("last deleted key = %q", backend.lastDeletedKey)
	}
}

func TestRedisLevelProgressCacheReturnsBackendAndJSONErrors(t *testing.T) {
	ctx := context.Background()
	cache := NewRedisLevelProgressCache(nil, "custom:", time.Minute)

	cache.backend = &levelProgressCacheBackendFake{getErr: errors.New("redis down")}
	if _, _, err := cache.Get(ctx, 42); err == nil || !strings.Contains(err.Error(), "redis down") {
		t.Fatalf("Get(redis error) error = %v", err)
	}

	cache.backend = &levelProgressCacheBackendFake{values: map[string]string{"custom:42": "not-json"}}
	if _, _, err := cache.Get(ctx, 42); err == nil || !strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("Get(json error) error = %v", err)
	}
}

type levelProgressProviderSpy struct {
	snapshots map[int64]LevelProgressSnapshot
	err       error
	callsByID map[int64]int
}

func (s *levelProgressProviderSpy) FetchLevelProgress(_ context.Context, discourseID int64) (LevelProgressSnapshot, error) {
	if s.callsByID == nil {
		s.callsByID = make(map[int64]int)
	}
	s.callsByID[discourseID]++
	if s.err != nil {
		return LevelProgressSnapshot{}, s.err
	}
	snapshot, ok := s.snapshots[discourseID]
	if !ok {
		return LevelProgressSnapshot{}, errors.New("missing snapshot")
	}
	return snapshot, nil
}

type memoryLevelProgressCache struct {
	values   map[int64]LevelProgressSnapshot
	setCalls int
}

func newMemoryLevelProgressCache() *memoryLevelProgressCache {
	return &memoryLevelProgressCache{values: make(map[int64]LevelProgressSnapshot)}
}

func (c *memoryLevelProgressCache) Get(_ context.Context, discourseID int64) (LevelProgressSnapshot, bool, error) {
	snapshot, ok := c.values[discourseID]
	return snapshot, ok, nil
}

func (c *memoryLevelProgressCache) Set(_ context.Context, snapshot LevelProgressSnapshot, _ time.Duration) error {
	c.setCalls++
	c.values[snapshot.DiscourseID] = snapshot
	return nil
}

func (c *memoryLevelProgressCache) Invalidate(_ context.Context, discourseID int64) error {
	delete(c.values, discourseID)
	return nil
}

type levelProgressCacheBackendFake struct {
	values         map[string]string
	getErr         error
	setErr         error
	deleteErr      error
	lastSetKey     string
	lastSetValue   string
	lastSetTTL     time.Duration
	lastDeletedKey string
}

func (b *levelProgressCacheBackendFake) Get(_ context.Context, key string) (string, error) {
	if b.getErr != nil {
		return "", b.getErr
	}
	value, ok := b.values[key]
	if !ok {
		return "", errLevelProgressCacheMiss
	}
	return value, nil
}

func (b *levelProgressCacheBackendFake) Set(_ context.Context, key, value string, ttl time.Duration) error {
	if b.setErr != nil {
		return b.setErr
	}
	if b.values == nil {
		b.values = make(map[string]string)
	}
	b.values[key] = value
	b.lastSetKey = key
	b.lastSetValue = value
	b.lastSetTTL = ttl
	return nil
}

func (b *levelProgressCacheBackendFake) Delete(_ context.Context, key string) error {
	if b.deleteErr != nil {
		return b.deleteErr
	}
	b.lastDeletedKey = key
	delete(b.values, key)
	return nil
}
