package domain

import (
	"testing"
	"time"
)

func TestApplicationTransitionFollowsReviewLifecycle(t *testing.T) {
	app := Application{ID: ApplicationID("app_1"), Status: StatusDraft}
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

	for _, target := range []ApplicationStatus{StatusPendingReview, StatusProvisioning, StatusApproved, StatusRevoked} {
		var err error
		if target == StatusPendingReview {
			err = app.Transition(target, now)
		} else if target == StatusProvisioning {
			err = app.Transition(target, now)
		} else if target == StatusApproved {
			err = app.Transition(target, now)
		} else {
			err = app.Transition(target, now)
		}
		if target == StatusPendingReview && err != nil {
			t.Fatalf("transition to %s: %v", target, err)
		}
		if target != StatusPendingReview && err == nil {
			t.Fatalf("transition to %s unexpectedly succeeded from %s", target, app.Status)
		}
		if target == StatusPendingReview {
			if app.Status != StatusPendingReview || !app.UpdatedAt.Equal(now) {
				t.Fatalf("app after submit = %#v", app)
			}
			break
		}
	}

	if err := app.Transition(StatusChangesRequested, now); err != nil {
		t.Fatalf("request changes: %v", err)
	}
	if err := app.Transition(StatusDraft, now); err != nil {
		t.Fatalf("return to draft: %v", err)
	}
	if err := app.Transition(StatusPendingReview, now); err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if err := app.Transition(StatusProvisioning, now); err != nil {
		t.Fatalf("provisioning: %v", err)
	}
	if err := app.Transition(StatusApproved, now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := app.Transition(StatusRevoked, now); err != nil {
		t.Fatalf("revoke: %v", err)
	}
}

func TestApplicationTransitionRejectsInvalidEdges(t *testing.T) {
	app := Application{Status: StatusDraft}
	if err := app.Transition(StatusApproved, time.Now()); err == nil {
		t.Fatal("draft -> approved succeeded, want invalid transition")
	}
}
