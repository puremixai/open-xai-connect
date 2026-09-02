package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsHandlerServesInternalDocumentation(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	mux := http.NewServeMux()
	RegisterDocsRoutes(mux, renderer)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /docs status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("GET /docs content-type = %q, want HTML", got)
	}
	body := response.Body.String()
	for _, marker := range []string{"XAI Connect 文档", `id="接入前提"`, `id="登录流程authorization-code--pkce"`, `id="安全说明"`} {
		if !strings.Contains(body, marker) {
			t.Fatalf("docs page missing %q: %s", marker, body)
		}
	}
	if strings.Contains(body, "github.com/puremixai/xai-connect") {
		t.Fatalf("docs page points back to GitHub: %s", body)
	}
}

func TestDocsHandlerRejectsUnsupportedMethods(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	mux := http.NewServeMux()
	RegisterDocsRoutes(mux, renderer)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/docs", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /docs status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
