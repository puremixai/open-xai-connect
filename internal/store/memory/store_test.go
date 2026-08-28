package memory

import (
	"context"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
)

func TestApplicationRepositoryCompareAndTransition(t *testing.T) {
	repo := NewApplicationRepository()
	app := domain.Application{
		ID:           domain.ApplicationID("app_1"),
		OwnerSubject: "sub_1",
		Name:         "Example",
		Status:       domain.StatusDraft,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repo.Transition(context.Background(), app.ID, domain.StatusApproved, domain.StatusRevoked, time.Now().UTC()); err == nil {
		t.Fatal("transition with stale state succeeded")
	}
	got, err := repo.Transition(context.Background(), app.ID, domain.StatusDraft, domain.StatusPendingReview, time.Now().UTC())
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if got.Status != domain.StatusPendingReview {
		t.Fatalf("status = %s, want %s", got.Status, domain.StatusPendingReview)
	}
}

func TestApplicationRepositoryRejectsDuplicateIDs(t *testing.T) {
	repo := NewApplicationRepository()
	app := domain.Application{ID: domain.ApplicationID("app_1"), OwnerSubject: "sub_1"}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if err := repo.Create(context.Background(), app); err == nil {
		t.Fatal("second Create() error = nil, want duplicate error")
	}
}

func TestOutboxReclaimsStaleClaim(t *testing.T) {
	repo := NewOutboxRepository()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if err := repo.Enqueue(context.Background(), domain.OutboxEvent{
		ID: "event_1", IdempotencyKey: "key_1", Kind: "test", AvailableAt: now.Add(-time.Hour), CreatedAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNext(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != "event_1" {
		t.Fatalf("claimed = %#v", claimed)
	}
	if _, err := repo.ClaimNext(context.Background(), now.Add(5*time.Minute)); err == nil {
		t.Fatal("fresh claim was unexpectedly reclaimed")
	}
	if _, err := repo.ClaimNext(context.Background(), now.Add(11*time.Minute)); err != nil {
		t.Fatalf("stale claim was not reclaimed: %v", err)
	}
}
