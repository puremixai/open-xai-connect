package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscoveryContainsOnlyPublicOIDCEndpoints(t *testing.T) {
	handler := NewDiscoveryHandler("https://connect.xai.run")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var document map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode discovery: %v", err)
	}
	if document["issuer"] != "https://connect.xai.run" ||
		document["userinfo_endpoint"] != "https://connect.xai.run/userinfo" ||
		document["authorization_endpoint"] != "https://connect.xai.run/oauth2/auth" {
		t.Fatalf("document = %#v", document)
	}
	if !strings.Contains(response.Body.String(), `"email"`) {
		t.Fatalf("discovery does not advertise email scope/claim: %s", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "4445") || strings.Contains(response.Body.String(), "admin") {
		t.Fatal("discovery exposes Hydra admin information")
	}
}

func TestJWKSProxyForwardsPublicResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/jwks.json" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"keys\":[]}"))
	}))
	defer upstream.Close()
	handler, err := NewJWKSProxy(upstream.URL, upstream.Client())
	if err != nil {
		t.Fatalf("NewJWKSProxy() error = %v", err)
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"keys\":[]}" {
		t.Fatalf("status/body = %d/%q", response.Code, response.Body.String())
	}
}
