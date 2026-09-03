package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

type MemoryStore struct {
	mu       sync.RWMutex
	data     map[[sha256.Size]byte]Session
	sliding  time.Duration
	absolute time.Duration
}

func NewMemoryStore(sliding, absolute time.Duration) *MemoryStore {
	return &MemoryStore{
		data:     make(map[[sha256.Size]byte]Session),
		sliding:  sliding,
		absolute: absolute,
	}
}

func (s *MemoryStore) Create(ctx context.Context, subject string, now time.Time) (Session, error) {
	if err := contextError(ctx); err != nil {
		return Session{}, err
	}
	if subject == "" || s.sliding <= 0 || s.absolute <= 0 {
		return Session{}, errors.New("session subject and positive TTLs are required")
	}
	rawID := make([]byte, 32)
	if _, err := rand.Read(rawID); err != nil {
		return Session{}, err
	}
	id := base64.RawURLEncoding.EncodeToString(rawID)
	now = now.UTC()
	session := Session{
		ID: id, UserSubject: subject, CreatedAt: now, LastSeenAt: now,
		ExpiresAt:         expiry(now, s.sliding, s.absolute),
		AbsoluteExpiresAt: now.Add(s.absolute),
	}
	s.mu.Lock()
	s.data[hashID(id)] = session
	s.mu.Unlock()
	return session, nil
}

func (s *MemoryStore) Get(ctx context.Context, id string, now time.Time) (Session, error) {
	if err := contextError(ctx); err != nil {
		return Session{}, err
	}
	key := hashID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.data[key]
	if !ok {
		return Session{}, ErrNotFound
	}
	if !validAt(session, now) {
		delete(s.data, key)
		return Session{}, ErrNotFound
	}
	session.ID = id
	return session, nil
}

func (s *MemoryStore) Touch(ctx context.Context, id string, now time.Time) (Session, error) {
	if err := contextError(ctx); err != nil {
		return Session{}, err
	}
	key := hashID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.data[key]
	if !ok || !validAt(session, now) {
		delete(s.data, key)
		return Session{}, ErrNotFound
	}
	now = now.UTC()
	session.LastSeenAt = now
	remaining := session.AbsoluteExpiresAt.Sub(now)
	if remaining <= 0 {
		delete(s.data, key)
		return Session{}, ErrNotFound
	}
	session.ExpiresAt = expiry(now, s.sliding, remaining)
	if session.ExpiresAt.After(session.AbsoluteExpiresAt) {
		session.ExpiresAt = session.AbsoluteExpiresAt
	}
	session.ID = id
	s.data[key] = session
	return session, nil
}

func (s *MemoryStore) MarkHomeVerified(ctx context.Context, id string, now time.Time) (Session, error) {
	if err := contextError(ctx); err != nil {
		return Session{}, err
	}
	key := hashID(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.data[key]
	if !ok || !validAt(session, now) {
		delete(s.data, key)
		return Session{}, ErrNotFound
	}
	now = now.UTC()
	session.HomeVerified = true
	session.LastSeenAt = now
	remaining := session.AbsoluteExpiresAt.Sub(now)
	if remaining <= 0 {
		delete(s.data, key)
		return Session{}, ErrNotFound
	}
	session.ExpiresAt = expiry(now, s.sliding, remaining)
	if session.ExpiresAt.After(session.AbsoluteExpiresAt) {
		session.ExpiresAt = session.AbsoluteExpiresAt
	}
	session.ID = id
	s.data[key] = session
	return session, nil
}

func (s *MemoryStore) Delete(ctx context.Context, id string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.data, hashID(id))
	s.mu.Unlock()
	return nil
}

func validAt(session Session, now time.Time) bool {
	now = now.UTC()
	return now.Before(session.ExpiresAt) && now.Before(session.AbsoluteExpiresAt)
}

func hashID(id string) [sha256.Size]byte {
	return sha256.Sum256([]byte(id))
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
