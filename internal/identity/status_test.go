package identity

import (
	"context"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store/memory"
)

type fakeProvider struct {
	snapshot UserSnapshot
	calls    int
}

func (p *fakeProvider) FetchUser(_ context.Context, _ int64) (UserSnapshot, error) {
	p.calls++
	return p.snapshot, nil
}

func TestStatusRefresherPersistsCurrentDiscourseStatus(t *testing.T) {
	users := memory.NewUserRepository()
	if err := users.Upsert(context.Background(), domain.User{
		Subject: domain.UserID("sub_1"), DiscourseID: 42, Username: "old",
		Active: true, TrustLevel: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	provider := &fakeProvider{snapshot: UserSnapshot{
		Subject: "sub_1", DiscourseID: 42, Username: "alice", Name: "Alice",
		TrustLevel: 2, Active: true, Silenced: true, Suspended: false,
	}}
	refresher := NewStatusRefresher(provider, users)

	status, err := refresher.CurrentStatus(context.Background(), "sub_1")
	if err != nil {
		t.Fatalf("CurrentStatus() error = %v", err)
	}
	if provider.calls != 1 || status.TrustLevel != 2 || !status.Silenced {
		t.Fatalf("provider calls/status = %d/%#v", provider.calls, status)
	}
	persisted, err := users.GetBySubject(context.Background(), domain.UserID("sub_1"))
	if err != nil {
		t.Fatalf("GetBySubject() error = %v", err)
	}
	if persisted.Username != "alice" || !persisted.Silenced {
		t.Fatalf("persisted user = %#v", persisted)
	}
}

func TestStatusRefresherRejectsSubjectMismatch(t *testing.T) {
	users := memory.NewUserRepository()
	_ = users.Upsert(context.Background(), domain.User{
		Subject: domain.UserID("sub_1"), DiscourseID: 42, Active: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	refresher := NewStatusRefresher(&fakeProvider{snapshot: UserSnapshot{
		Subject: "different", DiscourseID: 42, Active: true,
	}}, users)
	if _, err := refresher.CurrentStatus(context.Background(), "sub_1"); err == nil {
		t.Fatal("CurrentStatus() accepted mismatched subject")
	}
}

func TestStatusRefresherPreservesSubjectWhenPluginOmitsIt(t *testing.T) {
	users := memory.NewUserRepository()
	_ = users.Upsert(context.Background(), domain.User{
		Subject: domain.UserID("usr_stable"), DiscourseID: 42, Username: "old", Active: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	refresher := NewStatusRefresher(&fakeProvider{snapshot: UserSnapshot{
		DiscourseID: 42, Username: "alice", Active: true,
	}}, users)
	status, err := refresher.CurrentStatus(context.Background(), "usr_stable")
	if err != nil {
		t.Fatal(err)
	}
	if status.Subject != "usr_stable" {
		t.Fatalf("status subject = %q", status.Subject)
	}
	if _, err := users.GetBySubject(context.Background(), domain.UserID("usr_stable")); err != nil {
		t.Fatalf("stable subject was lost: %v", err)
	}
}
