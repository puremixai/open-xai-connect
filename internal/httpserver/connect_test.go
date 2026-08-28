package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connect.xai.run/internal/session"
)

func newConnectTestHandler(begin LoginBeginFunc, complete LoginCompleteFunc) *session.HTTPHandler {
	store := session.NewMemoryStore(time.Hour, 2*time.Hour)
	return session.NewHTTPHandler(store, "connect_session", true)
}

func TestConnectLoginRequiresChallengeAndRedirectsToDiscourse(t *testing.T) {
	var gotChallenge string
	handler := New(Dependencies{
		Register: func(mux *http.ServeMux) {
			RegisterConnectRoutes(mux, ConnectDependencies{
				BeginLogin: func(_ context.Context, challenge string) (string, error) {
					gotChallenge = challenge
					return "https://forum.example/session/sso?return=connect", nil
				},
			})
		},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/connect/login?login_challenge=abc", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusFound || response.Header().Get("Location") != "https://forum.example/session/sso?return=connect" {
		t.Fatalf("status/location = %d/%q", response.Code, response.Header().Get("Location"))
	}
	if gotChallenge != "abc" {
		t.Fatalf("challenge = %q", gotChallenge)
	}
}

func TestConnectCallbackEstablishesSessionAndRedirects(t *testing.T) {
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	handler := New(Dependencies{
		Register: func(mux *http.ServeMux) {
			RegisterConnectRoutes(mux, ConnectDependencies{
				Sessions: sessions,
				CompleteLogin: func(_ context.Context, _ *http.Request) (string, string, error) {
					return "sub_1", "/oauth/continue", nil
				},
			})
		},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/connect/callback?nonce=n1", nil)

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/oauth/continue" {
		t.Fatalf("status/location = %d/%q", response.Code, response.Header().Get("Location"))
	}
	if len(response.Result().Cookies()) != 1 {
		t.Fatal("callback did not set a session cookie")
	}
}

func TestConnectMeReturnsSubjectAndLogoutClearsSession(t *testing.T) {
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	handler := New(Dependencies{
		Register: func(mux *http.ServeMux) {
			RegisterConnectRoutes(mux, ConnectDependencies{Sessions: sessions})
		},
	})
	loginResponse := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/connect/callback", nil)
	if _, err := sessions.Establish(loginResponse, loginRequest, "sub_1"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	meRequest := httptest.NewRequest(http.MethodGet, "/connect/me", nil)
	meRequest.AddCookie(cookie)
	meResponse := httptest.NewRecorder()
	handler.ServeHTTP(meResponse, meRequest)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("me status = %d", meResponse.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(meResponse.Body.Bytes(), &body); err != nil || body["subject"] != "sub_1" {
		t.Fatalf("me body = %q", meResponse.Body.String())
	}
	logoutRequest := httptest.NewRequest(http.MethodPost, "/connect/logout", nil)
	logoutRequest.AddCookie(cookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusNoContent || logoutResponse.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout status/cookie = %d/%#v", logoutResponse.Code, logoutResponse.Result().Cookies())
	}
}
