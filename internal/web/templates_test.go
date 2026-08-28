package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRendererEscapesApplicationContentAndSetsSecurityHeaders(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	response := httptest.NewRecorder()
	err = renderer.Render(response, "app-form", map[string]any{
		"Title": "Create", "Name": "<script>alert(1)</script>",
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
