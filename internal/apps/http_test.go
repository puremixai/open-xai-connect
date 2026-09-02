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
	"connect.xai.run/internal/web"
)

type fakeLevelProgressLookup struct {
	snapshot identity.LevelProgressSnapshot
	err      error
	calls    int
	subjects []string
}

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

func TestHTTPHandlerLoadsLevelProgressForHomepage(t *testing.T) {
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
		t.Fatalf("homepage status = %d, body = %q", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if progress.calls != 1 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 1", progress.calls)
	}
	if len(progress.subjects) != 1 || progress.subjects[0] != "sub_1" {
		t.Fatalf("CurrentLevelProgress() subjects = %#v", progress.subjects)
	}
	if renderer.lastRender == nil || renderer.lastRender.name != "app-list" {
		t.Fatalf("render = %#v", renderer.lastRender)
	}
	levelProgress, ok := renderer.lastRender.data["LevelProgress"].(*identity.LevelProgressSnapshot)
	if !ok || levelProgress == nil {
		t.Fatalf("LevelProgress render data = %#v", renderer.lastRender.data["LevelProgress"])
	}
	if levelProgress.CurrentLevel.Label != "基础用户" || levelProgress.NextLevel == nil || levelProgress.NextLevel.Label != "成员" {
		t.Fatalf("LevelProgress snapshot = %#v", levelProgress)
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
	})
	mux := http.NewServeMux()
	RegisterRoutes(mux, handler)

	request := authenticatedRequest(t, sessions, "/")
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
	if progress.calls != 1 {
		t.Fatalf("CurrentLevelProgress() calls = %d, want 1", progress.calls)
	}
	if renderer.lastRender == nil || renderer.lastRender.name != "app-list" {
		t.Fatalf("render = %#v", renderer.lastRender)
	}
	levelProgress, ok := renderer.lastRender.data["LevelProgress"].(*identity.LevelProgressSnapshot)
	if !ok {
		t.Fatalf("LevelProgress render data type = %T", renderer.lastRender.data["LevelProgress"])
	}
	if levelProgress != nil {
		t.Fatalf("LevelProgress render data = %#v, want nil", levelProgress)
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
