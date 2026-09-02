package identity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientFetchUserSignsRequestAndDecodesMinimumFields(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte{}
		verifier, err := NewVerifier([]byte("shared-secret"), 5*time.Minute, NewMemoryNonceStore())
		if err != nil {
			t.Fatalf("NewVerifier() error = %v", err)
		}
		verifier.now = func() time.Time { return now }
		if err := verifier.Verify(r.Context(), r, body); err != nil {
			t.Fatalf("signed request verification failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(UserSnapshot{
			Subject: "sub_42", DiscourseID: 42, Username: "alice", Name: "Alice",
			Email: "alice@example.com", AvatarURL: "https://forum.example/avatar.png", TrustLevel: 1, Active: true,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, []byte("shared-secret"), server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.now = func() time.Time { return now }
	got, err := client.FetchUser(context.Background(), 42)
	if err != nil {
		t.Fatalf("FetchUser() error = %v", err)
	}
	if got.Subject != "sub_42" || got.Username != "alice" || got.Email != "alice@example.com" || got.TrustLevel != 1 {
		t.Fatalf("snapshot = %#v", got)
	}
}

func TestClientAcceptsDiscoursePayloadWithoutPublicSubject(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte{}
		verifier, _ := NewVerifier([]byte("shared-secret"), 5*time.Minute, NewMemoryNonceStore())
		verifier.now = func() time.Time { return now }
		if err := verifier.Verify(r.Context(), r, body); err != nil {
			t.Fatalf("signed request verification failed: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(UserSnapshot{
			DiscourseID: 42, Username: "alice", Active: true,
		})
	}))
	defer server.Close()
	client, err := NewClient(server.URL, []byte("shared-secret"), server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.now = func() time.Time { return now }
	if _, err := client.FetchUser(context.Background(), 42); err != nil {
		t.Fatalf("FetchUser() error = %v", err)
	}
}

func TestClientFetchesLevelProgressWithSignedGET(t *testing.T) {
	var gotPath, gotSignature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotSignature = r.Header.Get("X-Connect-Signature")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"schema_version":1,"discourse_id":42,"current_level":{"id":1,"key":"basic","label":"基础用户"},"next_level":{"id":2,"key":"member","label":"成员"},"promotion_mode":"automatic","requirements_met":false,"requirements":[],"blocking_conditions":[],"generated_at":"2026-09-02T08:00:00Z"}`)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, []byte("shared-secret"), server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.FetchLevelProgress(context.Background(), 42)
	if err != nil {
		t.Fatalf("FetchLevelProgress() error = %v", err)
	}
	if gotPath != "/connect/identity/users/42/level-progress" || gotSignature == "" {
		t.Fatalf("request path/signature = %q/%q", gotPath, gotSignature)
	}
}
