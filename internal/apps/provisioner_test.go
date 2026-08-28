package apps

import (
	"context"
	"errors"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/secrets"
	"connect.xai.run/internal/store"
	"connect.xai.run/internal/store/memory"
)

func TestProvisionerCreatesHydraClientEncryptsSecretAndCompletesEvent(t *testing.T) {
	apps := memory.NewApplicationRepository()
	app := domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "owner", Name: "Example",
		Status: domain.StatusProvisioning, CallbackURLs: []string{"https://app.example/callback"},
		VerifiedDomains: []string{"app.example"},
	}
	if err := apps.Create(context.Background(), app); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	outbox := memory.NewOutboxRepository()
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if err := outbox.Enqueue(context.Background(), domain.OutboxEvent{
		ID: "event_1", IdempotencyKey: "application:app_1:provision",
		Kind: "application.provision", ApplicationID: app.ID,
		AvailableAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	box, err := secrets.NewBox([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	hydraFake := hydra.NewFake()
	worker := NewProvisioner(ProvisionerDependencies{
		Apps: apps, Outbox: outbox, Audit: memory.NewAuditRepository(),
		Hydra: hydraFake, Box: box, Now: func() time.Time { return now },
	})
	processed, err := worker.RunOnce(context.Background())
	if err != nil || !processed {
		t.Fatalf("RunOnce() = %v/%v", processed, err)
	}
	updated, err := apps.Get(context.Background(), app.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if updated.Status != domain.StatusApproved || updated.ClientID == "" || updated.EncryptedClientSecret == "" {
		t.Fatalf("updated app = %#v", updated)
	}
	secret, err := box.Decrypt(updated.EncryptedClientSecret, string(updated.ID)+":"+string(updated.ClientID))
	if err != nil || secret != "secret_"+string(updated.ClientID) {
		t.Fatalf("stored secret = %q/%v", secret, err)
	}
	if len(hydraFake.Registrations) != 1 || hydraFake.Registrations[0].ResponseTypes[0] != "code" {
		t.Fatalf("Hydra registrations = %#v", hydraFake.Registrations)
	}
	if _, err := outbox.ClaimNext(context.Background(), now.Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("completed event was claimable")
	}
}

func TestProvisionerRetriesHydraFailureWithoutApprovingApp(t *testing.T) {
	apps := memory.NewApplicationRepository()
	app := domain.Application{ID: domain.ApplicationID("app_1"), OwnerSubject: "owner", Name: "Example", Status: domain.StatusProvisioning}
	_ = apps.Create(context.Background(), app)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	outbox := memory.NewOutboxRepository()
	_ = outbox.Enqueue(context.Background(), domain.OutboxEvent{
		ID: "event_1", IdempotencyKey: "application:app_1:provision", Kind: "application.provision",
		ApplicationID: app.ID, AvailableAt: now, CreatedAt: now,
	})
	box, _ := secrets.NewBox([]byte("01234567890123456789012345678901"))
	hydraFake := hydra.NewFake()
	hydraFake.Err = hydra.ErrUnavailable
	worker := NewProvisioner(ProvisionerDependencies{
		Apps: apps, Outbox: outbox, Hydra: hydraFake, Box: box, Now: func() time.Time { return now },
		RetryDelay: time.Minute,
	})
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil")
	}
	updated, _ := apps.Get(context.Background(), app.ID)
	if updated.Status != domain.StatusProvisioning {
		t.Fatalf("status = %s", updated.Status)
	}
	if _, err := outbox.ClaimNext(context.Background(), now.Add(30*time.Second)); err == nil {
		t.Fatal("retry event was available before retry delay")
	}
}
