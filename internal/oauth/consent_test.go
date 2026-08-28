package oauth

import (
	"context"
	"reflect"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/store/memory"
)

func TestFilterScopesDeduplicatesAndPreservesAllowedOrder(t *testing.T) {
	got, err := FilterScopes([]string{"profile", "openid", "profile", "community", "email"})
	if err != nil {
		t.Fatalf("FilterScopes() error = %v", err)
	}
	want := []string{"profile", "openid", "community", "email"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %#v, want %#v", got, want)
	}
}

func TestFilterScopesRejectsUnapprovedScope(t *testing.T) {
	if _, err := FilterScopes([]string{"openid", "unapproved"}); err == nil {
		t.Fatal("FilterScopes() accepted an unapproved scope")
	}
}

func TestConsentServiceIncludesEmailInIDTokenClaimsWhenGranted(t *testing.T) {
	hydraFake := hydra.NewFake()
	hydraFake.Consent = hydra.ConsentRequest{
		Challenge: "consent_email", Client: hydra.ClientInfo{ID: "client_email", Name: "Email App"},
		RequestedScope: []string{"openid", "email"}, Subject: "sub_1",
	}
	apps := memory.NewApplicationRepository()
	if err := apps.Create(context.Background(), domain.Application{
		ID: domain.ApplicationID("app_email"), OwnerSubject: "owner", Name: "Email App",
		Status: domain.StatusApproved, ClientID: domain.ClientID("client_email"),
	}); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	service := NewConsentService(hydraFake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Email: "alice@example.com", Active: true,
	}}, apps, memory.NewConsentRepository(), time.Hour)

	if _, err := service.Accept(context.Background(), "consent_email", "sub_1", []string{"openid", "email"}, false); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if got := hydraFake.LastConsent.IDTokenClaims["email"]; got != "alice@example.com" {
		t.Fatalf("ID token email claim = %#v, want alice@example.com", got)
	}
}

type fakeStatusLookup struct {
	status identity.StatusSnapshot
}

func (f fakeStatusLookup) CurrentStatus(context.Context, string) (identity.StatusSnapshot, error) {
	return f.status, nil
}

func TestConsentServicePersistsRememberedApprovedScopes(t *testing.T) {
	hydraFake := hydra.NewFake()
	hydraFake.Consent = hydra.ConsentRequest{
		Challenge: "consent_1", Client: hydra.ClientInfo{ID: "client_1", Name: "Example"},
		RequestedScope: []string{"openid", "profile"}, Subject: "sub_1",
	}
	apps := memory.NewApplicationRepository()
	if err := apps.Create(context.Background(), domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "sub_owner", Name: "Example",
		Status: domain.StatusApproved, ClientID: domain.ClientID("client_1"),
	}); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	consents := memory.NewConsentRepository()
	service := NewConsentService(hydraFake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: true, TrustLevel: 1,
	}}, apps, consents, time.Hour)

	redirect, err := service.Accept(context.Background(), "consent_1", "sub_1", []string{"openid", "profile"}, true)
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	if redirect == "" || len(hydraFake.LastConsent.GrantScope) != 2 || !hydraFake.LastConsent.Remember {
		t.Fatalf("redirect/acceptance = %q/%#v", redirect, hydraFake.LastConsent)
	}
	consent, err := consents.Get(context.Background(), domain.UserID("sub_1"), domain.ApplicationID("app_1"))
	if err != nil {
		t.Fatalf("Get() consent error = %v", err)
	}
	if !consent.Remember || !reflect.DeepEqual(consent.Scopes, []string{"openid", "profile"}) {
		t.Fatalf("consent = %#v", consent)
	}
}

func TestConsentServiceRejectsSuspendedUserAndUnrequestedScope(t *testing.T) {
	hydraFake := hydra.NewFake()
	hydraFake.Consent = hydra.ConsentRequest{
		Challenge: "consent_1", Client: hydra.ClientInfo{ID: "client_1"},
		RequestedScope: []string{"openid"}, Subject: "sub_1",
	}
	apps := memory.NewApplicationRepository()
	_ = apps.Create(context.Background(), domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "owner", Status: domain.StatusApproved,
		ClientID: domain.ClientID("client_1"),
	})
	service := NewConsentService(hydraFake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: false, Suspended: true,
	}}, apps, memory.NewConsentRepository(), time.Hour)
	if _, err := service.Accept(context.Background(), "consent_1", "sub_1", []string{"openid"}, false); err == nil {
		t.Fatal("Accept() allowed suspended user")
	}
	hydraFake.Consent.Subject = "sub_1"
	hydraFake.Consent.RequestedScope = []string{"openid", "profile"}
	service = NewConsentService(hydraFake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: true,
	}}, apps, memory.NewConsentRepository(), time.Hour)
	if _, err := service.Accept(context.Background(), "consent_1", "sub_1", []string{"profile"}, false); err == nil {
		t.Fatal("Accept() allowed scope absent from request")
	}
}

func TestConsentServiceDoesNotPersistWhenHydraAcceptFails(t *testing.T) {
	hydraFake := hydra.NewFake()
	hydraFake.Consent = hydra.ConsentRequest{
		Challenge: "consent_1", Client: hydra.ClientInfo{ID: "client_1"},
		RequestedScope: []string{"openid"}, Subject: "sub_1",
	}
	apps := memory.NewApplicationRepository()
	_ = apps.Create(context.Background(), domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "owner", Status: domain.StatusApproved,
		ClientID: domain.ClientID("client_1"),
	})
	consents := memory.NewConsentRepository()
	hydraFake.AcceptConsentErr = hydra.ErrUnavailable
	service := NewConsentService(hydraFake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", Active: true,
	}}, apps, consents, time.Hour)
	if _, err := service.Accept(context.Background(), "consent_1", "sub_1", []string{"openid"}, true); err == nil {
		t.Fatal("Accept() error = nil")
	}
	if _, err := consents.Get(context.Background(), domain.UserID("sub_1"), domain.ApplicationID("app_1")); err == nil {
		t.Fatal("consent was persisted after Hydra failure")
	}
}
