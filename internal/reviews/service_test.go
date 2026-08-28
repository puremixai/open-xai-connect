package reviews

import (
	"context"
	"errors"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store/memory"
)

type fakeReviewer struct {
	allowed bool
}

func (f fakeReviewer) IsReviewer(context.Context, string) (bool, error) {
	return f.allowed, nil
}

func (fakeReviewer) IsAdmin(context.Context, string) (bool, error) {
	return false, nil
}

type fakeReviewAccess struct {
	reviewer bool
	admin    bool
}

func (f fakeReviewAccess) IsReviewer(context.Context, string) (bool, error) {
	return f.reviewer, nil
}

func (f fakeReviewAccess) IsAdmin(context.Context, string) (bool, error) {
	return f.admin, nil
}

func seedPendingApp(t *testing.T) *memory.ApplicationRepository {
	t.Helper()
	repo := memory.NewApplicationRepository()
	if err := repo.Create(context.Background(), domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "owner", Name: "Example",
		Status: domain.StatusPendingReview, CallbackURLs: []string{"https://app.example/callback"},
		VerifiedDomains: []string{"app.example"},
	}); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	return repo
}

func TestApproveTransitionsAndEnqueuesIdempotentProvisioning(t *testing.T) {
	apps := seedPendingApp(t)
	outbox := memory.NewOutboxRepository()
	service := NewService(Dependencies{
		Apps: apps, Outbox: outbox, Audit: memory.NewAuditRepository(),
		Access: fakeReviewer{allowed: true}, Now: func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
	})
	if err := service.Approve(context.Background(), "reviewer", domain.ApplicationID("app_1"), "looks good"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	app, err := apps.Get(context.Background(), domain.ApplicationID("app_1"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if app.Status != domain.StatusProvisioning || app.ReviewedBy != "reviewer" || app.ReviewNote != "looks good" {
		t.Fatalf("app = %#v", app)
	}
	event, err := outbox.ClaimNext(context.Background(), service.now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if event.Kind != "application.provision" || event.IdempotencyKey != "application:app_1:provision" {
		t.Fatalf("event = %#v", event)
	}
	if err := service.Approve(context.Background(), "reviewer", domain.ApplicationID("app_1"), "duplicate"); err == nil {
		t.Fatal("second Approve() succeeded")
	}
}

func TestReviewerCannotApproveOwnAppOrWithoutReviewerRole(t *testing.T) {
	apps := seedPendingApp(t)
	service := NewService(Dependencies{Apps: apps, Outbox: memory.NewOutboxRepository(), Access: fakeReviewer{allowed: false}})
	if err := service.Approve(context.Background(), "reviewer", domain.ApplicationID("app_1"), "reason"); !errors.Is(err, ErrNotReviewer) {
		t.Fatalf("not reviewer error = %v", err)
	}
	service = NewService(Dependencies{Apps: apps, Outbox: memory.NewOutboxRepository(), Access: fakeReviewer{allowed: true}})
	if err := service.Approve(context.Background(), "owner", domain.ApplicationID("app_1"), "reason"); !errors.Is(err, ErrSelfReview) {
		t.Fatalf("self review error = %v", err)
	}
}

func TestAdminCanReviewAnyApplicationIncludingOwn(t *testing.T) {
	apps := seedPendingApp(t)
	service := NewService(Dependencies{
		Apps: apps, Outbox: memory.NewOutboxRepository(),
		Access: fakeReviewAccess{admin: true}, Now: func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) },
	})
	if pending, err := service.ListPending(context.Background(), "owner"); err != nil || len(pending) != 1 {
		t.Fatalf("admin ListPending() = %#v/%v", pending, err)
	}
	if err := service.Approve(context.Background(), "owner", domain.ApplicationID("app_1"), "admin review"); err != nil {
		t.Fatalf("admin Approve() error = %v", err)
	}
}

func TestRequestChangesReturnsApplicationToEditableState(t *testing.T) {
	apps := seedPendingApp(t)
	service := NewService(Dependencies{Apps: apps, Outbox: memory.NewOutboxRepository(), Access: fakeReviewer{allowed: true}})
	if err := service.RequestChanges(context.Background(), "reviewer", domain.ApplicationID("app_1"), "add privacy policy"); err != nil {
		t.Fatalf("RequestChanges() error = %v", err)
	}
	app, err := apps.Get(context.Background(), domain.ApplicationID("app_1"))
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if app.Status != domain.StatusChangesRequested || app.ReviewNote != "add privacy policy" {
		t.Fatalf("app = %#v", app)
	}
	pending, err := service.ListPending(context.Background(), "reviewer")
	if err != nil {
		t.Fatalf("ListPending() error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending applications = %#v", pending)
	}
}
