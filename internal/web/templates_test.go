package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"connect.xai.run/internal/domain"
)

func TestRendererIncludesRoleAwarePortalNavigation(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"Apps":   []domain.Application{},
		"Layout": Layout{Active: "apps", DisplayName: "Portal Admin", IsReviewer: true, IsAdmin: true},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, label := range []string{"应用总览", "应用审核", "运营概览", "Portal Admin"} {
		if !strings.Contains(body, label) {
			t.Fatalf("navigation missing %q in %q", label, body)
		}
	}
	for _, marker := range []string{"portal-shell", "portal-sidebar", "workspace-grid", "app-table", "onboarding-list", "创建应用"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("dashboard layout missing %q", marker)
		}
	}
	if asideIndex, navIndex := strings.Index(body, `<aside class="portal-sidebar">`), strings.Index(body, `<nav class="portal-nav"`); asideIndex < 0 || navIndex < asideIndex {
		t.Fatalf("portal navigation must be rendered inside the sidebar: %q", body)
	}
	if !strings.Contains(body, AssetsPath+"/portal.") {
		t.Fatalf("stylesheet link is not content-addressed: %q", body)
	}
}

func TestRendererUsesOneLinkForEachApplicationRow(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"Apps": []domain.Application{{
			ID:          "app_1",
			Name:        "Example App",
			Description: "Example description",
			Status:      domain.StatusApproved,
		}},
		"Layout": Layout{Active: "apps"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if got := strings.Count(body, `href="/connect/apps/app_1"`); got != 1 {
		t.Fatalf("application row has %d detail links, want one full-row link", got)
	}
	if !strings.Contains(body, `class="app-row-link"`) ||
		!strings.Contains(body, `aria-label="查看 Example App 详情"`) {
		t.Fatalf("application row is missing its labelled full-row link: %q", body)
	}
}

func TestRendererScopesConsentFormActionToValidatedCallbackOrigins(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	redirectURIs := []string{
		"https://mail.example/auth/callback",
		"https://MAIL.EXAMPLE/other",
		"https://portal.example:8443/callback",
		"http://insecure.example/callback",
		"https://localhost/callback",
		"https://127.0.0.1/callback",
		"https://user:pass@evil.example/callback",
		"https://evil.example/callback?next=1",
		"https://evil.example/callback#fragment",
		"https://*.evil.example/callback",
	}
	consent := httptest.NewRecorder()
	if err := renderer.RenderWithFormAction(consent, "consent", map[string]any{
		"Client": map[string]any{"Name": "Example"}, "Scopes": []string{"openid"},
		"Action": "/connect/consent", "CSRFToken": "csrf",
	}, redirectURIs); err != nil {
		t.Fatalf("RenderWithFormAction(consent) error = %v", err)
	}
	want := "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self' https://mail.example https://portal.example:8443"
	if csp := consent.Header().Get("Content-Security-Policy"); csp != want {
		t.Fatalf("consent CSP = %q, want %q", csp, want)
	}

	errorPage := httptest.NewRecorder()
	if err := renderer.RenderWithFormAction(errorPage, "error", map[string]any{
		"Title": "Error", "Message": "Denied", "Back": "/",
	}, redirectURIs); err != nil {
		t.Fatalf("RenderWithFormAction(error) error = %v", err)
	}
	if csp := errorPage.Header().Get("Content-Security-Policy"); strings.Contains(csp, "mail.example") || strings.Contains(csp, "portal.example") {
		t.Fatalf("non-consent CSP leaked callback origins: %q", csp)
	}
}

func TestRendererShowsProvisioningActionForExistingDraft(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"PageTitle": "应用详情",
		"Apps":      []domain.Application{{ID: "app_1", Name: "Draft", Status: domain.StatusDraft}},
		"Layout":    Layout{Active: "apps", CSRFToken: "csrf"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	if !strings.Contains(body, `action="/connect/apps/app_1/submit"`) || !strings.Contains(body, "开始上线") {
		t.Fatalf("draft provisioning action missing: %q", body)
	}
	for _, marker := range []string{"detail-layout", "action-rail", `href="/connect/apps/app_1/edit"`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("draft detail layout missing %q", marker)
		}
	}
}

func TestRendererShowsCredentialActionsForApprovedApplication(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-list", map[string]any{
		"PageTitle":   "应用详情",
		"ConfirmedAt": int64(1787918400),
		"Apps":        []domain.Application{{ID: "app_1", Name: "Approved", Status: domain.StatusApproved, ClientID: "client_1"}},
		"Layout":      Layout{Active: "apps", CSRFToken: "csrf"},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`href="/connect/apps/app_1/edit"`,
		`action="/connect/apps/app_1/secret"`,
		`action="/connect/apps/app_1/rotate-secret"`,
		`action="/connect/apps/app_1/delete"`,
		`name="confirmed_at" value="1787918400"`,
		"查看 Client Secret", "轮换 Secret", "删除应用",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("approved credential action missing %q: %s", expected, body)
		}
	}
}

func TestRendererEscapesApplicationContentAndSetsSecurityHeaders(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-form", map[string]any{
		"Title": "Create", "Name": "<script>alert(1)</script>", "Layout": Layout{},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(response.Body.String(), "<script>alert(1)</script>") ||
		!strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("rendered body is not escaped: %q", response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") == "" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func TestRendererRejectsUnknownTemplate(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	if err := renderer.Render(httptest.NewRecorder(), "missing", nil); err == nil {
		t.Fatal("Render() accepted unknown template")
	}
}

func TestRendererCanSetNonSuccessStatusBeforeRendering(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	if err := renderer.RenderStatus(response, 403, "error", map[string]any{
		"Title": "Forbidden", "Message": "Denied", "Back": "/",
	}); err != nil {
		t.Fatalf("RenderStatus() error = %v", err)
	}
	if response.Code != 403 {
		t.Fatalf("status = %d", response.Code)
	}
}
