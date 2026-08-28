package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store/memory"
)

func TestEventConsumerVerifiesAndAppliesSignedStatusEvent(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	users := memory.NewUserRepository()
	_ = users.Upsert(context.Background(), domain.User{
		Subject: "sub_1", DiscourseID: 42, Username: "alice", Active: true,
		CreatedAt: now, UpdatedAt: now,
	})
	verifier, _ := NewVerifier([]byte("shared-secret"), 5*time.Minute, NewMemoryNonceStore())
	verifier.now = func() time.Time { return now }
	consumer := NewEventConsumer(verifier, users, NewMemoryNonceStore())
	payload, _ := json.Marshal(StatusEvent{
		EventID: "event-1", UserID: 42, EventType: "user_status_changed",
		OccurredAt: now, Status: UserSnapshot{
			DiscourseID: 42, Username: "alice", Email: "alice@example.com", TrustLevel: 2, Active: false, Suspended: true,
		},
	})
	request := httptest.NewRequest(http.MethodPost, "https://connect.example/connect/identity/events", bytes.NewReader(payload))
	request.Header.Set("X-Connect-Timestamp", "1787918400")
	request.Header.Set("X-Connect-Nonce", "nonce-1")
	request.Header.Set("X-Connect-Signature", Sign([]byte("shared-secret"), request.Method, request.URL.RequestURI(), now, "nonce-1", payload))

	if err := consumer.ServeHTTP(httptest.NewRecorder(), request); err != nil {
		t.Fatalf("ServeHTTP() error = %v", err)
	}
	updated, err := users.GetBySubject(context.Background(), "sub_1")
	if err != nil {
		t.Fatalf("GetBySubject() error = %v", err)
	}
	if updated.Active || !updated.Suspended || updated.Email != "alice@example.com" || updated.TrustLevel != 2 {
		t.Fatalf("updated user = %#v", updated)
	}
	if err := consumer.ServeHTTP(httptest.NewRecorder(), request); err == nil {
		t.Fatal("ServeHTTP() accepted duplicate event")
	}
}
