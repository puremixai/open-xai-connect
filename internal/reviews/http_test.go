package reviews

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/store/memory"
	"connect.xai.run/internal/web"
)

type reviewStatusLookup struct {
	status identity.StatusSnapshot
}

func (l reviewStatusLookup) CurrentStatus(context.Context, string) (identity.StatusSnapshot, error) {
	return l.status, nil
}

func TestReviewHTTPHandlerRequiresCSRFAndAppliesDecision(t *testing.T) {
	apps := seedPendingApp(t)
	service := NewService(Dependencies{
		Apps: apps, Outbox: memory.NewOutboxRepository(), Access: fakeReviewer{allowed: true},
	})
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, _ := session.NewCSRF([]byte("csrf-secret-012345"))
	handler := NewHTTPHandler(HTTPDependencies{Service: service, Sessions: sessions, CSRF: csrf, Renderer: renderer})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)
	loginResponse := httptest.NewRecorder()
	sessionValue, err := sessions.Establish(loginResponse, httptest.NewRequest(http.MethodGet, "/", nil), "reviewer")
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	token, _ := csrf.Token(sessionValue.ID)
	form := url.Values{"csrf_token": {token}, "decision": {"changes"}, "reason": {"add privacy policy"}}
	request := httptest.NewRequest(http.MethodPost, "/connect/review/app_1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	app, _ := apps.Get(context.Background(), "app_1")
	if app.Status != "changes_requested" {
		t.Fatalf("status = %s", app.Status)
	}
}

func TestAdminOverviewShowsStatusCountsForAdmin(t *testing.T) {
	apps := memory.NewApplicationRepository()
	for i, status := range []domain.ApplicationStatus{domain.StatusPendingReview, domain.StatusApproved} {
		if err := apps.Create(context.Background(), domain.Application{
			ID: domain.ApplicationID("app_admin_" + string(rune('a'+i))), OwnerSubject: "owner",
			Name: "Admin app", Status: status,
		}); err != nil {
			t.Fatalf("seed app: %v", err)
		}
	}
	service := NewService(Dependencies{Apps: apps, Outbox: memory.NewOutboxRepository(), Access: fakeReviewer{allowed: true}})
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, _ := session.NewCSRF([]byte("csrf-secret-012345"))
	handler := NewHTTPHandler(HTTPDependencies{
		Service: service, Status: reviewStatusLookup{status: identity.StatusSnapshot{
			Subject: "admin", Name: "Portal Admin", Active: true, Admin: true,
		}}, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)
	loginResponse := httptest.NewRecorder()
	if _, err := sessions.Establish(loginResponse, httptest.NewRequest(http.MethodGet, "/", nil), "admin"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	request := httptest.NewRequest(http.MethodGet, "/connect/admin", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body = %q", response.Code, response.Body.String())
	}
	for _, value := range []string{"运营概览", "待审核", "已上线", "Portal Admin"} {
		if !strings.Contains(response.Body.String(), value) {
			t.Fatalf("admin page missing %q: %q", value, response.Body.String())
		}
	}
}
