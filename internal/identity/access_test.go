package identity

import (
	"context"
	"testing"

	"connect.xai.run/internal/store/memory"
)

type accessStatusLookup struct {
	status StatusSnapshot
}

func (l accessStatusLookup) CurrentStatus(context.Context, string) (StatusSnapshot, error) {
	return l.status, nil
}

func TestAccessLookupUsesInternalReviewerAndAdminBooleans(t *testing.T) {
	lookup := NewAccessLookup(accessStatusLookup{status: StatusSnapshot{
		Subject: "sub_reviewer", Active: true, Reviewer: true, Admin: false,
	}}, memory.NewUserRepository())
	reviewer, err := lookup.IsReviewer(context.Background(), "sub_reviewer")
	if err != nil || !reviewer {
		t.Fatalf("IsReviewer() = %v/%v", reviewer, err)
	}
	admin, err := lookup.IsAdmin(context.Background(), "sub_reviewer")
	if err != nil || admin {
		t.Fatalf("IsAdmin() = %v/%v", admin, err)
	}
}
