package identity

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerifierAcceptsValidSignedRequest(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	nonces := NewMemoryNonceStore()
	verifier, err := NewVerifier([]byte("shared-secret"), 5*time.Minute, nonces)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }
	body := []byte("{\"subject\":\"sub_1\"}")
	request := httptest.NewRequest(http.MethodPost, "https://connect.example/connect/identity/events", bytes.NewReader(body))
	nonce := "nonce-1"
	request.Header.Set("X-Connect-Timestamp", "1787918400")
	request.Header.Set("X-Connect-Nonce", nonce)
	request.Header.Set("X-Connect-Signature", Sign([]byte("shared-secret"), request.Method, request.URL.RequestURI(), now, nonce, body))

	if err := verifier.Verify(context.Background(), request, body); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestVerifierRejectsExpiredAndReplayedRequests(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	nonces := NewMemoryNonceStore()
	verifier, err := NewVerifier([]byte("shared-secret"), 5*time.Minute, nonces)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }
	body := []byte("payload")
	makeRequest := func(timestamp time.Time, nonce string) *http.Request {
		request := httptest.NewRequest(http.MethodPost, "https://connect.example/events", bytes.NewReader(body))
		request.Header.Set("X-Connect-Timestamp", "1787918400")
		request.Header.Set("X-Connect-Nonce", nonce)
		request.Header.Set("X-Connect-Signature", Sign([]byte("shared-secret"), request.Method, request.URL.RequestURI(), timestamp, nonce, body))
		return request
	}

	if err := verifier.Verify(context.Background(), makeRequest(now.Add(-6*time.Minute), "expired"), body); err == nil {
		t.Fatal("Verify() accepted expired request")
	}
	first := makeRequest(now, "replayed")
	if err := verifier.Verify(context.Background(), first, body); err != nil {
		t.Fatalf("first Verify() error = %v", err)
	}
	if err := verifier.Verify(context.Background(), makeRequest(now, "replayed"), body); err == nil {
		t.Fatal("Verify() accepted replayed request")
	}
}

func TestVerifierRejectsInvalidSignatureAndMissingHeaders(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	verifier, err := NewVerifier([]byte("shared-secret"), 5*time.Minute, NewMemoryNonceStore())
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }
	body := []byte("payload")
	request := httptest.NewRequest(http.MethodPost, "https://connect.example/events", bytes.NewReader(body))
	request.Header.Set("X-Connect-Timestamp", "1787918400")
	request.Header.Set("X-Connect-Nonce", "nonce")
	request.Header.Set("X-Connect-Signature", Sign([]byte("wrong-secret"), request.Method, request.URL.RequestURI(), now, "nonce", body))
	if err := verifier.Verify(context.Background(), request, body); err == nil {
		t.Fatal("Verify() accepted invalid signature")
	}
	request.Header.Del("X-Connect-Signature")
	if err := verifier.Verify(context.Background(), request, body); err == nil {
		t.Fatal("Verify() accepted missing signature")
	}
}
