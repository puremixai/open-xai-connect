package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientAcceptsSiteverifySuccessForExpectedActionAndHostname(t *testing.T) {
	var gotSecret, gotResponse string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		gotSecret = r.Form.Get("secret")
		gotResponse = r.Form.Get("response")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":  true,
			"action":   "home",
			"hostname": "connect.xai.run",
		})
	}))
	defer server.Close()

	client, err := New("secret-value", []string{"connect.xai.run"}, server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	client.endpoint = server.URL

	if err := client.Verify(context.Background(), "token-value", "home"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if gotSecret != "secret-value" || gotResponse != "token-value" {
		t.Fatalf("siteverify form = secret:%q response:%q", gotSecret, gotResponse)
	}
}

func TestClientRejectsActionAndHostnameMismatches(t *testing.T) {
	cases := []struct {
		name     string
		action   string
		hostname string
	}{
		{name: "action", action: "other", hostname: "connect.xai.run"},
		{name: "hostname", action: "home", hostname: "evil.example"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success":  true,
					"action":   tc.action,
					"hostname": tc.hostname,
				})
			}))
			defer server.Close()

			client, err := New("secret-value", []string{"connect.xai.run"}, server.Client())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			client.endpoint = server.URL

			if err := client.Verify(context.Background(), "token-value", "home"); !errors.Is(err, ErrRejected) {
				t.Fatalf("Verify() error = %v, want ErrRejected", err)
			}
		})
	}
}

func TestClientRejectsMissingAndOversizedTokensWithoutCallingSiteverify(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer server.Close()

	client, err := New("secret-value", []string{"connect.xai.run"}, server.Client())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	client.endpoint = server.URL

	for _, token := range []string{"", strings.Repeat("x", 2049)} {
		if err := client.Verify(context.Background(), token, "home"); !errors.Is(err, ErrRejected) {
			t.Fatalf("Verify(%d-byte token) error = %v, want ErrRejected", len(token), err)
		}
	}
	if called {
		t.Fatal("siteverify was called for an invalid token")
	}
}

func TestNewRequiresSecretAndExpectedHostname(t *testing.T) {
	if _, err := New("", []string{"connect.xai.run"}, nil); err == nil {
		t.Fatal("New() with empty secret succeeded")
	}
	if _, err := New("secret-value", nil, nil); err == nil {
		t.Fatal("New() with no hostnames succeeded")
	}
}
