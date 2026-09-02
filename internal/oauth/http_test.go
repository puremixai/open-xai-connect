package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/store/memory"
	"connect.xai.run/internal/web"
)

type consentStatus struct{}

func (consentStatus) CurrentStatus(context.Context, string) (identity.StatusSnapshot, error) {
	return identity.StatusSnapshot{Subject: "usr_1", Active: true}, nil
}

func TestConsentHTTPHandlerRendersAndAccepts(t *testing.T) {
	h := hydra.NewFake()
	h.Consent = hydra.ConsentRequest{
		Challenge: "consent-1", Subject: "usr_1", Client: hydra.ClientInfo{
			ID: "client-1", Name: "Example", RedirectURIs: []string{"https://mail.example/auth/callback"},
		},
		RequestedScope: []string{"openid", "profile"},
	}
	apps := memory.NewApplicationRepository()
	now := time.Now().UTC()
	if err := apps.Create(context.Background(), domain.Application{ID: "app-1", OwnerSubject: "usr-owner", Name: "Example", ClientID: "client-1", Status: domain.StatusApproved, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	consents := memory.NewConsentRepository()
	service := NewConsentService(h, consentStatus{}, apps, consents, time.Hour)
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", false)
	csrf, err := session.NewCSRF([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}
	handler := NewConsentHTTPHandler(ConsentHTTPDependencies{Service: service, Sessions: sessions, CSRF: csrf, Renderer: renderer})

	seedResponse := httptest.NewRecorder()
	seedRequest := httptest.NewRequest(http.MethodGet, "https://connect.example/connect/consent", nil)
	if _, err := sessions.Establish(seedResponse, seedRequest, "usr_1"); err != nil {
		t.Fatal(err)
	}
	cookie := seedResponse.Result().Cookies()[0]
	get := httptest.NewRequest(http.MethodGet, "https://connect.example/connect/consent?consent_challenge=consent-1", nil)
	get.AddCookie(cookie)
	getResponse := httptest.NewRecorder()
	handler.handle(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), "Example") {
		t.Fatalf("GET status/body = %d/%q", getResponse.Code, getResponse.Body.String())
	}
	if csp := getResponse.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "form-action 'self' https://mail.example") {
		t.Fatalf("GET CSP = %q, want approved callback origin", csp)
	}
	sid, _ := sessions.SessionID(get)
	token, _ := csrf.Token(sid)
	post := httptest.NewRequest(http.MethodPost, "https://connect.example/connect/consent?consent_challenge=consent-1", strings.NewReader("csrf_token="+token+"&decision=allow&scope=openid+profile"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(cookie)
	postResponse := httptest.NewRecorder()
	handler.handle(postResponse, post)
	if postResponse.Code != http.StatusSeeOther || postResponse.Header().Get("Location") != h.RedirectURL {
		t.Fatalf("POST status/location = %d/%q", postResponse.Code, postResponse.Header().Get("Location"))
	}
}

func TestValidateHTTPSRedirect(t *testing.T) {
	for _, value := range []string{"https://client.example/callback", "http://client.example/callback", "/relative", "https://user:pass@client.example/callback"} {
		err := validateHTTPSRedirect(value)
		if strings.HasPrefix(value, "https://client") && err != nil {
			t.Fatalf("validateHTTPSRedirect(%q) = %v", value, err)
		}
		if !strings.HasPrefix(value, "https://client") && err == nil {
			t.Fatalf("validateHTTPSRedirect(%q) unexpectedly accepted", value)
		}
	}
}
