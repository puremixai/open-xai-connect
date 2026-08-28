package hydra

import (
	"context"
	"testing"
)

func TestFakeClientCreatesConfidentialCodeClient(t *testing.T) {
	fake := NewFake()
	credentials, err := fake.CreateClient(context.Background(), ClientRegistration{
		ClientName: "Example", RedirectURIs: []string{"https://app.example/callback"},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, Scope: "openid profile",
		TokenEndpointAuthMethod: "client_secret_basic",
	})
	if err != nil {
		t.Fatalf("CreateClient() error = %v", err)
	}
	if credentials.ID == "" || credentials.Secret == "" {
		t.Fatalf("credentials = %#v", credentials)
	}
	if len(fake.Registrations) != 1 || fake.Registrations[0].ResponseTypes[0] != "code" {
		t.Fatalf("registrations = %#v", fake.Registrations)
	}
}

func TestFakeClientReturnsConfiguredIntrospection(t *testing.T) {
	fake := NewFake()
	fake.Tokens["access-token"] = TokenIntrospection{
		Active: true, Subject: "sub_1", ClientID: "client_1", Scope: "openid profile",
	}
	token, err := fake.Introspect(context.Background(), "access-token")
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !token.Active || token.Subject != "sub_1" {
		t.Fatalf("token = %#v", token)
	}
}
