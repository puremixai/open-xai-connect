package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

var ErrIdentityMismatch = errors.New("Discourse identity response did not match Connect subject")

type StatusRefresher struct {
	provider Provider
	users    store.UserRepository
	now      func() time.Time
}

func NewStatusRefresher(provider Provider, users store.UserRepository) *StatusRefresher {
	return &StatusRefresher{provider: provider, users: users, now: time.Now}
}

func (r *StatusRefresher) CurrentStatus(ctx context.Context, subject string) (StatusSnapshot, error) {
	if r == nil || r.provider == nil || r.users == nil {
		return StatusSnapshot{}, errors.New("identity status refresher is not initialized")
	}
	user, err := r.users.GetBySubject(ctx, domain.UserID(subject))
	if err != nil {
		return StatusSnapshot{}, err
	}
	snapshot, err := r.provider.FetchUser(ctx, user.DiscourseID)
	if err != nil {
		return StatusSnapshot{}, fmt.Errorf("fetch current Discourse status: %w", err)
	}
	if (snapshot.Subject != "" && snapshot.Subject != subject) || snapshot.DiscourseID != user.DiscourseID {
		return StatusSnapshot{}, ErrIdentityMismatch
	}
	now := r.now().UTC()
	persisted := domain.User{
		Subject: domain.UserID(snapshot.Subject), DiscourseID: snapshot.DiscourseID,
		Username: snapshot.Username, Name: snapshot.Name, AvatarURL: snapshot.AvatarURL,
		TrustLevel: snapshot.TrustLevel, Active: snapshot.Active,
		Silenced: snapshot.Silenced, Suspended: snapshot.Suspended,
		Reviewer: snapshot.Reviewer, Admin: snapshot.Admin,
		CreatedAt: user.CreatedAt, UpdatedAt: now,
	}
	if err := r.users.Upsert(ctx, persisted); err != nil {
		return StatusSnapshot{}, fmt.Errorf("persist current Discourse status: %w", err)
	}
	return StatusSnapshot{
		Subject: snapshot.Subject, TrustLevel: snapshot.TrustLevel,
		Active: snapshot.Active, Silenced: snapshot.Silenced, Suspended: snapshot.Suspended,
		Reviewer: snapshot.Reviewer, Admin: snapshot.Admin,
	}, nil
}
