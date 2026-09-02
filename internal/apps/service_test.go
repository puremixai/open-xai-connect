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
		Apps:   repo, Outbox: memory.NewOutboxRepository(), Audit: memory.NewAuditRepository(), RequireReview: true,
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

func TestCreateDraftStartsProvisioningWithoutReview(t *testing.T) {
	repo := memory.NewApplicationRepository()
	outbox := memory.NewOutboxRepository()
	service := NewService(Dependencies{
		Status: appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		Apps:   repo, Outbox: outbox, Audit: memory.NewAuditRepository(), Box: mustTestBox(), Hydra: hydra.NewFake(),
	})
	service.now = func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) }
	app, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput())
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if app.Status != domain.StatusProvisioning {
		t.Fatalf("CreateDraft() status = %s, want provisioning", app.Status)
	}
	event, err := outbox.ClaimNext(context.Background(), service.now())
	if err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
	}
	if event.Kind != "application.provision" || event.ApplicationID != app.ID {
		t.Fatalf("provision event = %#v", event)
	}
}

func TestSubmitStartsProvisioningForAnExistingDraftWithoutReview(t *testing.T) {
	repo := memory.NewApplicationRepository()
	app := domain.Application{
		ID: "app_existing", OwnerSubject: "sub_1", Name: "Existing draft", Status: domain.StatusDraft,
		CallbackURLs: []string{"https://app.example/callback"}, VerifiedDomains: []string{"app.example"},
	}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	outbox := memory.NewOutboxRepository()
	service := NewService(Dependencies{
		Status: appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		Apps:   repo, Outbox: outbox, Audit: memory.NewAuditRepository(), Box: mustTestBox(), Hydra: hydra.NewFake(),
	})
	service.now = func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) }
	updated, err := service.Submit(context.Background(), "sub_1", app.ID)
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if updated.Status != domain.StatusProvisioning {
		t.Fatalf("Submit() status = %s, want provisioning", updated.Status)
	}
	if _, err := outbox.ClaimNext(context.Background(), service.now()); err != nil {
		t.Fatalf("ClaimNext() error = %v", err)
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
	if len(fake.Updates) != 1 || fake.Updates[0].ClientID != "client_1" || fake.Updates[0].Scope != "openid profile email community offline_access" {
		t.Fatalf("Hydra updates = %#v", fake.Updates)
	}
}

func TestUpdateApprovedApplicationSyncsHydraWithoutRotatingSecret(t *testing.T) {
	service, repo := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	service.now = func() time.Time { return time.Date(2026, 8, 28, 13, 0, 0, 0, time.UTC) }
	app := domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "sub_1", Name: "Example",
		Description: "Original description", LogoURL: "/assets/logo.png",
		CallbackURLs: []string{"https://app.example/callback"}, VerifiedDomains: []string{"app.example"},
		Status: domain.StatusApproved, ClientID: domain.ClientID("client_1"), SecretVersion: 2,
	}
	var err error
	app.EncryptedClientSecret, err = service.box.Encrypt("existing-secret", secretAssociatedData(app))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if err := repo.Create(context.Background(), app); err != nil {
		t.Fatalf("seed approved app: %v", err)
	}

	input := DraftInput{
		Name: "Updated Example", Description: "Updated description", LogoURL: "/assets/updated.png",
		CallbackURLs: []string{"https://updated.example/oauth/callback"}, VerifiedDomains: []string{"updated.example"},
	}
	updated, err := service.UpdateDraft(context.Background(), "sub_1", app.ID, input)
	if err != nil {
		t.Fatalf("UpdateDraft() error = %v", err)
	}
	if updated.Status != domain.StatusApproved {
		t.Fatalf("updated status = %s, want approved", updated.Status)
	}
	if updated.Name != input.Name || updated.Description != input.Description || updated.LogoURL != input.LogoURL ||
		updated.CallbackURLs[0] != input.CallbackURLs[0] || updated.VerifiedDomains[0] != input.VerifiedDomains[0] {
		t.Fatalf("updated app = %#v", updated)
	}
	if updated.EncryptedClientSecret != app.EncryptedClientSecret || updated.SecretVersion != app.SecretVersion {
		t.Fatalf("approved credentials changed during metadata update = %#v", updated)
	}
	secret, err := service.box.Decrypt(updated.EncryptedClientSecret, secretAssociatedData(updated))
	if err != nil || secret != "existing-secret" {
		t.Fatalf("existing secret = %q/%v", secret, err)
	}
	fake := service.hydra.(*hydra.Fake)
	if len(fake.Updates) != 1 {
		t.Fatalf("Hydra updates = %#v, want one update", fake.Updates)
	}
	registration := fake.Updates[0]
	if registration.ClientID != "client_1" || registration.ClientName != input.Name || registration.ClientSecret != "existing-secret" ||
		registration.LogoURI != input.LogoURL || registration.RedirectURIs[0] != input.CallbackURLs[0] || registration.Owner != "sub_1" {
		t.Fatalf("Hydra registration = %#v", registration)
	}
}
