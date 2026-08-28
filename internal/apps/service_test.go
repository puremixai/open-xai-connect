package apps

import (
	"context"
	"errors"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/secrets"
	"connect.xai.run/internal/store/memory"
)

type appStatusLookup struct {
	status identity.StatusSnapshot
}

func (l appStatusLookup) CurrentStatus(context.Context, string) (identity.StatusSnapshot, error) {
	return l.status, nil
}

func validDraftInput() DraftInput {
	return DraftInput{
		Name: "Example", Description: "Example app",
		CallbackURLs:    []string{"https://app.example/callback"},
		VerifiedDomains: []string{"app.example"},
	}
}

func newAppService(status identity.StatusSnapshot) (*Service, *memory.ApplicationRepository) {
	repo := memory.NewApplicationRepository()
	service := NewService(Dependencies{
		Status: appStatusLookup{status: status},
		Apps:   repo, Audit: memory.NewAuditRepository(),
		Box: mustTestBox(), Hydra: hydra.NewFake(),
		MaxOpen: 3, SensitiveWindow: 5 * time.Minute,
	})
	service.now = func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) }
	return service, repo
}

func mustTestBox() *secrets.Box {
	box, err := secrets.NewBox([]byte("01234567890123456789012345678901"))
	if err != nil {
		panic(err)
	}
	return box
}

func TestCreateDraftRequiresTL1ActiveNonSilencedUser(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 0})
	if _, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput()); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("TL0 error = %v, want ErrNotEligible", err)
	}
	service, _ = newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1, Silenced: true})
	if _, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput()); !errors.Is(err, ErrNotEligible) {
		t.Fatalf("silenced error = %v, want ErrNotEligible", err)
	}
	service, repo := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	app, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput())
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if app.Status != domain.StatusDraft || app.OwnerSubject != "sub_1" || app.ID == "" {
		t.Fatalf("draft = %#v", app)
	}
	for i := 0; i < 2; i++ {
		input := validDraftInput()
		input.Name = "Example " + string(rune('B'+i))
		if _, err := service.CreateDraft(context.Background(), "sub_1", input); err != nil {
			t.Fatalf("CreateDraft(%d) error = %v", i, err)
		}
	}
	if _, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput()); !errors.Is(err, ErrOpenApplicationLimit) {
		t.Fatalf("fourth CreateDraft() error = %v, want ErrOpenApplicationLimit", err)
	}
	if count, _ := repo.CountOpenByOwner(context.Background(), "sub_1"); count != 3 {
		t.Fatalf("open count = %d", count)
	}
}

func TestCreateDraftAllowsActiveConnectAdminBelowTL1(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{
		Subject: "sub_admin", Active: true, TrustLevel: 0, Admin: true,
	})
	if _, err := service.CreateDraft(context.Background(), "sub_admin", validDraftInput()); err != nil {
		t.Fatalf("admin CreateDraft() error = %v", err)
	}
}

func TestSubmitChecksOwnershipStateAndCallbackDomain(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	input := validDraftInput()
	input.CallbackURLs = []string{"http://app.example/callback"}
	if _, err := service.CreateDraft(context.Background(), "sub_1", input); err == nil {
		t.Fatal("CreateDraft() accepted non-HTTPS callback")
	}
	app, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput())
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := service.Submit(context.Background(), "other", app.ID); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("other owner error = %v, want ErrNotOwner", err)
	}
	submitted, err := service.Submit(context.Background(), "sub_1", app.ID)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if submitted.Status != domain.StatusPendingReview {
		t.Fatalf("status = %s", submitted.Status)
	}
	if _, err := service.Submit(context.Background(), "sub_1", app.ID); err == nil {
		t.Fatal("second Submit() succeeded")
	}
}

func TestViewSecretRequiresRecentConfirmationAndDecryptsWithAssociatedData(t *testing.T) {
	service, repo := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	encrypted, err := service.box.Encrypt("secret-value", "app_1:client_1")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	app := domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "sub_1", Name: "Example",
		Status: domain.StatusApproved, ClientID: domain.ClientID("client_1"),
		EncryptedClientSecret: encrypted, SecretVersion: 1,
	}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	now := service.now()
	if _, err := service.ViewSecret(context.Background(), "sub_1", app.ID, now.Add(-time.Hour)); !errors.Is(err, ErrSensitiveConfirmation) {
		t.Fatalf("old confirmation error = %v", err)
	}
	secret, err := service.ViewSecret(context.Background(), "sub_1", app.ID, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ViewSecret() error = %v", err)
	}
	if secret != "secret-value" {
		t.Fatalf("secret = %q", secret)
	}
}

func TestRotateSecretRequiresConfirmationAndInvalidatesOldSecret(t *testing.T) {
	service, repo := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	encrypted, err := service.box.Encrypt("old-secret", "app_1:client_1")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	app := domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "sub_1", Name: "Example",
		Status: domain.StatusApproved, ClientID: domain.ClientID("client_1"),
		EncryptedClientSecret: encrypted, SecretVersion: 1,
		CallbackURLs:    []string{"https://app.example/callback"},
		VerifiedDomains: []string{"app.example"},
	}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	now := service.now()
	if _, err := service.RotateSecret(context.Background(), "sub_1", app.ID, now.Add(-time.Minute)); err != nil {
		t.Fatalf("RotateSecret() error = %v", err)
	}
	updated, err := repo.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.SecretVersion != 2 || updated.EncryptedClientSecret == encrypted {
		t.Fatalf("updated secret metadata = %#v", updated)
	}
	secret, err := service.box.Decrypt(updated.EncryptedClientSecret, "app_1:client_1")
	if err != nil || secret != "rotated_client_1" {
		t.Fatalf("rotated secret = %q/%v", secret, err)
	}
	fake := service.hydra.(*hydra.Fake)
	if len(fake.Updates) != 1 || fake.Updates[0].ClientID != "client_1" {
		t.Fatalf("Hydra updates = %#v", fake.Updates)
	}
}
