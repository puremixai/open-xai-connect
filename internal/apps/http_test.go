package apps

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/web"
)

func TestHTTPHandlerProtectsAppCreationWithSessionAndCSRF(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	handler := NewHTTPHandler(HTTPDependencies{
		Service: service, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	loginResponse := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/connect/callback", nil)
	sessionValue, err := sessions.Establish(loginResponse, loginRequest, "sub_1")
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	token, err := csrf.Token(sessionValue.ID)
	if err != nil {
		t.Fatalf("CSRF token: %v", err)
	}
	form := url.Values{
		"csrf_token": {token}, "name": {"Example"}, "description": {"Demo"},
		"callbacks": {"https://app.example/callback"}, "domains": {"app.example"},
	}
	request := httptest.NewRequest(http.MethodPost, "/connect/apps", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d, body = %q", response.Code, response.Body.String())
	}

	badFormValues := url.Values{}
	for key, values := range form {
		badFormValues[key] = append([]string(nil), values...)
	}
	badFormValues.Set("csrf_token", "invalid")
	badRequest := httptest.NewRequest(http.MethodPost, "/connect/apps", strings.NewReader(badFormValues.Encode()))
	badRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	badRequest.AddCookie(cookie)
	badForm := httptest.NewRecorder()
	mux.ServeHTTP(badForm, badRequest)
	if badForm.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF status = %d", badForm.Code)
	}
}

func TestSecretActionDisablesCaching(t *testing.T) {
	handler := &HTTPHandler{}
	request := httptest.NewRequest(http.MethodPost, "/connect/apps/app_1/secret", nil)
	response := httptest.NewRecorder()

	handler.viewSecret(response, request, "sub_1", "app_1")

	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHTTPHandlerAllowsOwnerToEditAnUnpublishedApplication(t *testing.T) {
	service, repo := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	app, err := service.CreateDraft(nil, "sub_1", validDraftInput())
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	handler := NewHTTPHandler(HTTPDependencies{Service: service, Sessions: sessions, CSRF: csrf, Renderer: renderer})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	loginResponse := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/connect/callback", nil)
	sessionValue, err := sessions.Establish(loginResponse, loginRequest, "sub_1")
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	token, err := csrf.Token(sessionValue.ID)
	if err != nil {
		t.Fatalf("csrf token: %v", err)
	}

	editRequest := httptest.NewRequest(http.MethodGet, "/connect/apps/"+string(app.ID)+"/edit", nil)
	editRequest.AddCookie(cookie)
	editResponse := httptest.NewRecorder()
	mux.ServeHTTP(editResponse, editRequest)
	if editResponse.Code != http.StatusOK || !strings.Contains(editResponse.Body.String(), "编辑应用") || !strings.Contains(editResponse.Body.String(), app.Name) {
		t.Fatalf("edit response status = %d, body = %q", editResponse.Code, editResponse.Body.String())
	}

	form := url.Values{
		"csrf_token": {token}, "name": {"Updated Example"}, "description": {"Updated application"},
		"callbacks": {"https://app.example/updated"}, "domains": {"app.example"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/connect/apps/"+string(app.ID)+"/edit", strings.NewReader(form.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRequest.AddCookie(cookie)
	updateResponse := httptest.NewRecorder()
	mux.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusSeeOther {
		t.Fatalf("update status = %d, body = %q", updateResponse.Code, updateResponse.Body.String())
	}
	updated, err := repo.Get(nil, app.ID)
	if err != nil {
		t.Fatalf("read updated app: %v", err)
	}
	if updated.Name != "Updated Example" || updated.CallbackURLs[0] != "https://app.example/updated" {
		t.Fatalf("updated app = %#v", updated)
	}
}
