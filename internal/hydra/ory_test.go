package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOryClientMapsConfidentialClientRegistration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/clients" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["client_name"] != "Example" || body["token_endpoint_auth_method"] != "client_secret_basic" {
			t.Fatalf("client fields = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"client_id\":\"client_1\",\"client_secret\":\"secret_1\"}"))
	}))
	defer server.Close()

	client, err := NewOryClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewOryClient() error = %v", err)
	}
	credentials, err := client.CreateClient(context.Background(), ClientRegistration{
		ClientName: "Example", RedirectURIs: []string{"https://app.example/callback"},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, Scope: "openid profile",
		TokenEndpointAuthMethod: "client_secret_basic",
	})
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}
	if credentials.ID != "client_1" || credentials.Secret != "secret_1" {
		t.Fatalf("credentials = %#v", credentials)
	}
}

func TestOryClientRejectsConsentWithoutClientMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"challenge\":\"c1\",\"client\":null,\"requested_scope\":[\"openid\"],\"subject\":\"sub_1\"}"))
	}))
	defer server.Close()
	client, err := NewOryClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewOryClient() error = %v", err)
	}
	if _, err := client.GetConsentRequest(context.Background(), "c1"); err == nil {
		t.Fatal("GetConsentRequest() accepted missing client metadata")
	}
}
