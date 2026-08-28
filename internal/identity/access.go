package identity

import (
	"context"
	"errors"

	"connect.xai.run/internal/store"
)

type AccessLookup struct {
	status StatusLookup
	users  store.UserRepository
}

func NewAccessLookup(status StatusLookup, users store.UserRepository) *AccessLookup {
	return &AccessLookup{status: status, users: users}
}

func (l *AccessLookup) IsReviewer(ctx context.Context, subject string) (bool, error) {
	snapshot, err := l.snapshot(ctx, subject)
	return snapshot.Reviewer, err
}

func (l *AccessLookup) IsAdmin(ctx context.Context, subject string) (bool, error) {
	snapshot, err := l.snapshot(ctx, subject)
	return snapshot.Admin, err
}

func (l *AccessLookup) snapshot(ctx context.Context, subject string) (StatusSnapshot, error) {
	if l == nil || l.status == nil || subject == "" {
		return StatusSnapshot{}, errors.New("identity access lookup is not initialized")
	}
	snapshot, err := l.status.CurrentStatus(ctx, subject)
	if err != nil {
		return StatusSnapshot{}, err
	}
	if snapshot.Subject != "" && snapshot.Subject != subject {
		return StatusSnapshot{}, ErrIdentityMismatch
	}
	return snapshot, nil
}
