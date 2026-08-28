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
