package apps

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
	"connect.xai.run/internal/turnstile"
	"connect.xai.run/internal/web"
)

type fakeLevelProgressLookup struct {
	snapshot identity.LevelProgressSnapshot
	err      error
	calls    int
	subjects []string
}

type fakeTurnstileValidator struct {
	err    error
	calls  int
	token  string
	action string
}

func (f *fakeTurnstileValidator) Verify(_ context.Context, token, action string) error {
	f.calls++
	f.token = token
	f.action = action
	return f.err
}

var _ turnstile.Validator = (*fakeTurnstileValidator)(nil)

func (f *fakeLevelProgressLookup) CurrentLevelProgress(_ context.Context, subject string) (identity.LevelProgressSnapshot, error) {
	f.calls++
	f.subjects = append(f.subjects, subject)
	return f.snapshot, f.err
}

type renderCall struct {
	name string
	data map[string]any
}

type captureRenderer struct {
	base       pageRenderer
	lastRender *renderCall
}

func (s *captureRenderer) Render(w http.ResponseWriter, name string, data any) error {
	renderData, ok := data.(map[string]any)
	if ok {
		s.lastRender = &renderCall{name: name, data: renderData}
	}
	if s.base == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("rendered"))
		return nil
	}
	return s.base.Render(w, name, data)
}

func (s *captureRenderer) RenderStatus(w http.ResponseWriter, status int, name string, data any) error {
	renderData, ok := data.(map[string]any)
	if ok {
		s.lastRender = &renderCall{name: name, data: renderData}
	}
	if s.base == nil {
		w.WriteHeader(status)
		return nil
	}
	return s.base.RenderStatus(w, status, name, data)
}

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

func TestHTTPHandlerShowsTurnstileGateOnFirstHomepageVisit(t *testing.T) {
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	progress := &fakeLevelProgressLookup{}
	handler := NewHTTPHandler(HTTPDependencies{
		Status:        appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		LevelProgress: progress, Sessions: sessions, CSRF: csrf, Renderer: renderer,
		Turnstile: &fakeTurnstileValidator{}, TurnstileSiteKey: "0x4AAAAAAElnfHQEWJo0Sdjl",
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	request := authenticatedRequest(t, sessions, "/")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/connect/home/verify" {
		t.Fatalf("homepage gate redirect = %d/%q, body = %q", response.Code, response.Header().Get("Location"), response.Body.String())
	}

	gateRequest := httptest.NewRequest(http.MethodGet, "/connect/home/verify", nil)
	for _, cookie := range request.Cookies() {
		gateRequest.AddCookie(cookie)
	}
	gateResponse := httptest.NewRecorder()
	mux.ServeHTTP(gateResponse, gateRequest)
	if gateResponse.Code != http.StatusOK {
		t.Fatalf("verification page status = %d, body = %q", gateResponse.Code, gateResponse.Body.String())
	}
	if got := gateResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("verification page Cache-Control = %q, want no-store", got)
	}
	body := gateResponse.Body.String()
	if !strings.Contains(body, `class="cf-turnstile"`) || !strings.Contains(body, `data-action="home"`) {
		t.Fatalf("verification page missing Turnstile widget: %q", body)
	}
	if strings.Contains(body, "用户等级进度") || strings.Contains(body, `class="portal-shell"`) {
		t.Fatalf("homepage content/layout rendered before verification: %q", body)
	}
	if progress.calls != 0 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 0", progress.calls)
	}
}

func TestHTTPHandlerAcceptsHomepageTurnstileAndRendersContentAfterVerification(t *testing.T) {
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	progress := &fakeLevelProgressLookup{}
	validator := &fakeTurnstileValidator{}
	handler := NewHTTPHandler(HTTPDependencies{
		Status:        appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		LevelProgress: progress, Sessions: sessions, CSRF: csrf, Renderer: renderer,
		Turnstile: validator, TurnstileSiteKey: "0x4AAAAAAElnfHQEWJo0Sdjl",
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	loginResponse := httptest.NewRecorder()
	sessionValue, err := sessions.Establish(loginResponse, httptest.NewRequest(http.MethodGet, "/", nil), "sub_1")
	if err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	csrfToken, err := csrf.Token(sessionValue.ID)
	if err != nil {
		t.Fatalf("csrf.Token() error = %v", err)
	}
	form := url.Values{"csrf_token": {csrfToken}, "cf-turnstile-response": {"fresh-token"}}
	verifyRequest := httptest.NewRequest(http.MethodPost, "/connect/home/verify", strings.NewReader(form.Encode()))
	verifyRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	verifyRequest.AddCookie(cookie)
	verifyResponse := httptest.NewRecorder()
	mux.ServeHTTP(verifyResponse, verifyRequest)

	if verifyResponse.Code != http.StatusSeeOther || verifyResponse.Header().Get("Location") != "/" {
		t.Fatalf("verify response = %d/%q, body = %q", verifyResponse.Code, verifyResponse.Header().Get("Location"), verifyResponse.Body.String())
	}
	if validator.calls != 1 || validator.token != "fresh-token" || validator.action != "home" {
		t.Fatalf("Turnstile call = %d/%q/%q", validator.calls, validator.token, validator.action)
	}

	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(cookie)
	homeResponse := httptest.NewRecorder()
	mux.ServeHTTP(homeResponse, homeRequest)
	if homeResponse.Code != http.StatusOK || !strings.Contains(homeResponse.Body.String(), "用户等级进度") {
		t.Fatalf("verified homepage = %d/%q", homeResponse.Code, homeResponse.Body.String())
	}
	if progress.calls != 1 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 1", progress.calls)
	}

	gateAgainRequest := httptest.NewRequest(http.MethodGet, "/connect/home/verify", nil)
	gateAgainRequest.AddCookie(cookie)
	gateAgainResponse := httptest.NewRecorder()
	mux.ServeHTTP(gateAgainResponse, gateAgainRequest)
	if gateAgainResponse.Code != http.StatusSeeOther || gateAgainResponse.Header().Get("Location") != "/" {
		t.Fatalf("verified session gate response = %d/%q", gateAgainResponse.Code, gateAgainResponse.Header().Get("Location"))
	}
}

func TestHTTPHandlerKeepsHomepageBlockedWhenTurnstileFails(t *testing.T) {
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	validator := &fakeTurnstileValidator{err: turnstile.ErrRejected}
	handler := NewHTTPHandler(HTTPDependencies{
		Sessions: sessions, CSRF: csrf, Renderer: renderer,
		Turnstile: validator, TurnstileSiteKey: "0x4AAAAAAElnfHQEWJo0Sdjl",
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	loginResponse := httptest.NewRecorder()
	sessionValue, err := sessions.Establish(loginResponse, httptest.NewRequest(http.MethodGet, "/", nil), "sub_1")
	if err != nil {
		t.Fatalf("Establish() error = %v", err)
	}
	cookie := loginResponse.Result().Cookies()[0]
	csrfToken, err := csrf.Token(sessionValue.ID)
	if err != nil {
		t.Fatalf("csrf.Token() error = %v", err)
	}
	form := url.Values{"csrf_token": {csrfToken}, "cf-turnstile-response": {"rejected-token"}}
	verifyRequest := httptest.NewRequest(http.MethodPost, "/connect/home/verify", strings.NewReader(form.Encode()))
	verifyRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	verifyRequest.AddCookie(cookie)
	verifyResponse := httptest.NewRecorder()
	mux.ServeHTTP(verifyResponse, verifyRequest)
	if verifyResponse.Code != http.StatusForbidden {
		t.Fatalf("failed verification status = %d, body = %q", verifyResponse.Code, verifyResponse.Body.String())
	}

	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(cookie)
	homeResponse := httptest.NewRecorder()
	mux.ServeHTTP(homeResponse, homeRequest)
	if homeResponse.Code != http.StatusSeeOther || homeResponse.Header().Get("Location") != "/connect/home/verify" {
		t.Fatalf("failed verification did not return to gate: %d/%q", homeResponse.Code, homeResponse.Header().Get("Location"))
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
	updated.Status = domain.StatusApproved
	updated.ClientID = domain.ClientID("client_1")
	if err := repo.Save(nil, updated); err != nil {
		t.Fatalf("save approved app: %v", err)
	}
	approvedEditRequest := httptest.NewRequest(http.MethodGet, "/connect/apps/"+string(app.ID)+"/edit", nil)
	approvedEditRequest.AddCookie(cookie)
	approvedEditResponse := httptest.NewRecorder()
	mux.ServeHTTP(approvedEditResponse, approvedEditRequest)
	if approvedEditResponse.Code != http.StatusOK || !strings.Contains(approvedEditResponse.Body.String(), "编辑应用") {
		t.Fatalf("approved edit response status = %d, body = %q", approvedEditResponse.Code, approvedEditResponse.Body.String())
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/connect/apps/"+string(app.ID)+"/delete", strings.NewReader(url.Values{"csrf_token": {token}}.Encode()))
	deleteRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteRequest.AddCookie(cookie)
	deleteResponse := httptest.NewRecorder()
	mux.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusSeeOther || deleteResponse.Header().Get("Location") != "/connect/apps" {
		t.Fatalf("delete response status = %d, location = %q, body = %q", deleteResponse.Code, deleteResponse.Header().Get("Location"), deleteResponse.Body.String())
	}
	if _, err := repo.Get(nil, app.ID); err == nil {
		t.Fatal("deleted application is still present")
	}
}

func TestHTTPHandlerDoesNotLoadLevelProgressForAppOverview(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	if _, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput()); err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	progress := &fakeLevelProgressLookup{
		snapshot: identity.LevelProgressSnapshot{
			SchemaVersion: 1,
			DiscourseID:   42,
			CurrentLevel:  identity.LevelInfo{ID: 1, Key: "basic", Label: "基础用户"},
			NextLevel:     &identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
			PromotionMode: "automatic",
			RequirementsMet: func() *bool {
				value := false
				return &value
			}(),
			GeneratedAt: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
		},
	}
	renderer := &captureRenderer{}
	handler := NewHTTPHandler(HTTPDependencies{
		Service: service, Status: appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		LevelProgress: progress, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	request := authenticatedRequest(t, sessions, "/connect/apps")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("app overview status = %d, body = %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if progress.calls != 0 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 0", progress.calls)
	}
	if renderer.lastRender == nil || renderer.lastRender.name != "app-list" {
		t.Fatalf("render = %#v", renderer.lastRender)
	}
	if _, ok := renderer.lastRender.data["LevelProgress"]; ok {
		t.Fatalf("app overview render data unexpectedly includes LevelProgress: %#v", renderer.lastRender.data["LevelProgress"])
	}
}

func TestHTTPHandlerRendersLevelProgressOnlyAtRoot(t *testing.T) {
	renderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	progress := &fakeLevelProgressLookup{
		snapshot: identity.LevelProgressSnapshot{
			SchemaVersion: 1,
			DiscourseID:   42,
			CurrentLevel:  identity.LevelInfo{ID: 1, Key: "basic", Label: "基础用户"},
			NextLevel:     &identity.LevelInfo{ID: 2, Key: "member", Label: "成员"},
			PromotionMode: "automatic",
			RequirementsMet: func() *bool {
				value := false
				return &value
			}(),
			Requirements: []identity.LevelRequirement{
				{Key: "topics_entered", Label: "进入主题", Group: "activity", Current: 2, Target: 10, Operator: "at_least", Unit: "count"},
			},
			GeneratedAt: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
		},
	}
	handler := NewHTTPHandler(HTTPDependencies{
		Status:        appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		LevelProgress: progress, Sessions: sessions, CSRF: csrf, Renderer: renderer,
		Turnstile: &fakeTurnstileValidator{}, TurnstileSiteKey: "0x4AAAAAAElnfHQEWJo0Sdjl",
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	request := authenticatedRequest(t, sessions, "/")
	if err := sessions.MarkHomeVerified(request); err != nil {
		t.Fatalf("MarkHomeVerified() error = %v", err)
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("root status = %d, body = %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if progress.calls != 1 || len(progress.subjects) != 1 || progress.subjects[0] != "sub_1" {
		t.Fatalf("CurrentLevelProgress() calls/subjects = %d/%#v, want 1/[sub_1]", progress.calls, progress.subjects)
	}
	body := response.Body.String()
	for _, marker := range []string{"用户等级进度", "当前等级", "目标等级", "达到目标等级的条件", "基础用户", "成员"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("root body missing %q: %q", marker, body)
		}
	}
	for _, marker := range []string{"我的应用", "应用列表", "当前账号拥有", "workspace-grid", "app-table", "onboarding-list"} {
		if strings.Contains(body, marker) {
			t.Fatalf("root body unexpectedly contains app-list marker %q: %q", marker, body)
		}
	}
}

func TestHTTPHandlerDegradesWhenLevelProgressLookupFails(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	input := validDraftInput()
	input.Name = "Regression Example"
	if _, err := service.CreateDraft(context.Background(), "sub_1", input); err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	sessions := session.NewHTTPHandler(session.NewMemoryStore(time.Hour, 2*time.Hour), "connect_session", true)
	csrf, err := session.NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	progress := &fakeLevelProgressLookup{err: context.DeadlineExceeded}
	baseRenderer, err := web.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	renderer := &captureRenderer{base: baseRenderer}
	handler := NewHTTPHandler(HTTPDependencies{
		Service: service, Status: appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		LevelProgress: progress, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	request := authenticatedRequest(t, sessions, "/connect/apps")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("homepage status = %d, body = %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if progress.calls != 0 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 0", progress.calls)
	}
	if renderer.lastRender == nil || renderer.lastRender.name != "app-list" {
		t.Fatalf("render = %#v", renderer.lastRender)
	}
	if _, ok := renderer.lastRender.data["LevelProgress"]; ok {
		t.Fatalf("app overview render data unexpectedly includes LevelProgress: %#v", renderer.lastRender.data["LevelProgress"])
	}
	body := response.Body.String()
	for _, marker := range []string{
		"我的应用",
		"应用列表",
		"创建应用、查看上线状态、管理 OIDC 凭证。",
		"Regression Example",
		"workspace-grid",
		"app-table",
		"onboarding-list",
		`href="/connect/apps/`,
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("homepage body missing %q: %q", marker, body)
		}
	}
}

func TestHTTPHandlerDoesNotLoadLevelProgressForAppDetail(t *testing.T) {
	service, _ := newAppService(identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1})
	app, err := service.CreateDraft(context.Background(), "sub_1", validDraftInput())
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
	progress := &fakeLevelProgressLookup{}
	handler := NewHTTPHandler(HTTPDependencies{
		Service: service, Status: appStatusLookup{status: identity.StatusSnapshot{Subject: "sub_1", Active: true, TrustLevel: 1}},
		LevelProgress: progress, Sessions: sessions, CSRF: csrf, Renderer: renderer,
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	request := authenticatedRequest(t, sessions, "/connect/apps/"+string(app.ID))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("detail status = %d, body = %q", response.Code, response.Body.String())
	}
	if progress.calls != 0 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 0", progress.calls)
	}
}

func authenticatedRequest(t *testing.T, sessions *session.HTTPHandler, target string) *http.Request {
	t.Helper()
	loginResponse := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/connect/callback", nil)
	if _, err := sessions.Establish(loginResponse, loginRequest, "sub_1"); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(loginResponse.Result().Cookies()[0])
	return request
}
