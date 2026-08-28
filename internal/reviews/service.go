package reviews

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

var (
	ErrNotReviewer    = errors.New("reviewer permission required")
	ErrSelfReview     = errors.New("reviewer cannot review their own application")
	ErrReasonRequired = errors.New("review reason is required")
	ErrInvalidState   = errors.New("application is not pending review")
)

type AccessController interface {
	IsReviewer(context.Context, string) (bool, error)
}

type Dependencies struct {
	Apps   store.ApplicationRepository
	Outbox store.OutboxRepository
	Audit  store.AuditRepository
	Access AccessController
	Now    func() time.Time
}

type Service struct {
	apps   store.ApplicationRepository
	outbox store.OutboxRepository
	audit  store.AuditRepository
	access AccessController
	now    func() time.Time
}

func NewService(deps Dependencies) *Service {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &Service{apps: deps.Apps, outbox: deps.Outbox, audit: deps.Audit, access: deps.Access, now: now}
}

func (s *Service) ListPending(ctx context.Context, reviewer string) ([]domain.Application, error) {
	if err := s.requireReviewer(ctx, reviewer); err != nil {
		return nil, err
	}
	if s.apps == nil {
		return nil, errors.New("application repository is not initialized")
	}
	return s.apps.ListByStatus(ctx, domain.StatusPendingReview)
}

func (s *Service) Approve(ctx context.Context, reviewer string, id domain.ApplicationID, reason string) error {
	if err := s.prepareReview(ctx, reviewer, id, reason); err != nil {
		return err
	}
	if s.outbox == nil {
		return errors.New("outbox repository is not initialized")
	}
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.apps.Transition(ctx, id, domain.StatusPendingReview, domain.StatusProvisioning, s.now().UTC()); err != nil {
		return err
	}
	app.ReviewedBy, app.ReviewNote, app.Status, app.UpdatedAt = reviewer, strings.TrimSpace(reason), domain.StatusProvisioning, s.now().UTC()
	if err := s.apps.Save(ctx, app); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"application_id": string(id)})
	if err := s.outbox.Enqueue(ctx, domain.OutboxEvent{
		ID: newEventID(), IdempotencyKey: "application:" + string(id) + ":provision",
		Kind: "application.provision", ApplicationID: id, Payload: payload,
		AvailableAt: s.now().UTC(), CreatedAt: s.now().UTC(),
	}); err != nil {
		return err
	}
	return s.auditEvent(ctx, reviewer, id, "application.approved")
}

func (s *Service) Reject(ctx context.Context, reviewer string, id domain.ApplicationID, reason string) error {
	return s.decide(ctx, reviewer, id, reason, domain.StatusRejected, "application.rejected")
}

func (s *Service) RequestChanges(ctx context.Context, reviewer string, id domain.ApplicationID, reason string) error {
	return s.decide(ctx, reviewer, id, reason, domain.StatusChangesRequested, "application.changes_requested")
}

func (s *Service) decide(ctx context.Context, reviewer string, id domain.ApplicationID, reason string, target domain.ApplicationStatus, action string) error {
	if err := s.prepareReview(ctx, reviewer, id, reason); err != nil {
		return err
	}
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.apps.Transition(ctx, id, domain.StatusPendingReview, target, s.now().UTC()); err != nil {
		return err
	}
	app.Status, app.ReviewedBy, app.ReviewNote, app.UpdatedAt = target, reviewer, strings.TrimSpace(reason), s.now().UTC()
	if err := s.apps.Save(ctx, app); err != nil {
		return err
	}
	return s.auditEvent(ctx, reviewer, id, action)
}

func (s *Service) prepareReview(ctx context.Context, reviewer string, id domain.ApplicationID, reason string) error {
	if err := s.requireReviewer(ctx, reviewer); err != nil {
		return err
	}
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return err
	}
	if app.OwnerSubject == reviewer {
		return ErrSelfReview
	}
	if app.Status != domain.StatusPendingReview {
		return ErrInvalidState
	}
	return nil
}

func (s *Service) requireReviewer(ctx context.Context, subject string) error {
	if s == nil || s.access == nil || subject == "" {
		return ErrNotReviewer
	}
	allowed, err := s.access.IsReviewer(ctx, subject)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrNotReviewer
	}
	return nil
}

func (s *Service) auditEvent(ctx context.Context, actor string, id domain.ApplicationID, action string) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Append(ctx, domain.AuditEvent{
		ID: newEventID(), ActorSubject: actor, Action: action,
		ApplicationID: id, Metadata: map[string]string{"result": "success"},
		CreatedAt: s.now().UTC(),
	})
}

func newEventID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "event_fallback"
	}
	return "event_" + base64.RawURLEncoding.EncodeToString(raw)
}
