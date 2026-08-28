package session

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("session not found")

type Session struct {
	ID                string
	UserSubject       string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	ExpiresAt         time.Time
	AbsoluteExpiresAt time.Time
}

type Store interface {
	Create(context.Context, string, time.Time) (Session, error)
	Get(context.Context, string, time.Time) (Session, error)
	Touch(context.Context, string, time.Time) (Session, error)
	Delete(context.Context, string) error
}

func expiry(now time.Time, sliding, absolute time.Duration) time.Time {
	candidate := now.Add(sliding)
	limit := now.Add(absolute)
	if candidate.After(limit) {
		return limit
	}
	return candidate
}
