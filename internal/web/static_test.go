package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAssetHandlerServesStylesheetsWithCaching(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAssetRoutes(mux)

	for _, name := range []string{"portal.css", "consent.css", "error.css"} {
		request := httptest.NewRequest(http.MethodGet, AssetsPath+"/"+name, nil)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", name, response.Code)
		}
		if got := response.Header().Get("Content-Type"); !strings.Contains(got, "text/css") {
			t.Fatalf("%s content-type = %q", name, got)
		}
		etag := response.Header().Get("ETag")
		if etag == "" {
			t.Fatalf("%s missing ETag", name)
		}
		if got := response.Header().Get("Cache-Control"); got == "" {
			t.Fatalf("%s missing Cache-Control", name)
		}

		revalidate := httptest.NewRequest(http.MethodGet, AssetsPath+"/"+name, nil)
		revalidate.Header.Set("If-None-Match", etag)
		cached := httptest.NewRecorder()
		mux.ServeHTTP(cached, revalidate)
		if cached.Code != http.StatusNotModified {
			t.Fatalf("%s conditional status = %d, want 304", name, cached.Code)
		}
	}
}

func TestAssetHandlerRejectsUnknownFilesAndMethods(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAssetRoutes(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, AssetsPath+"/missing.css", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing file status = %d, want 404", response.Code)
	}

	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, AssetsPath+"/portal.css", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", response.Code)
	}
}

func TestPortalStylesheetCoversClassesUsedByTemplates(t *testing.T) {
	stylesheet, err := staticFS.ReadFile("static/portal.css")
	if err != nil {
		t.Fatalf("read portal.css: %v", err)
	}
	css := string(stylesheet)
	for _, class := range []string{
		"portal-topbar", "portal-brand", "portal-nav", "portal-main",
		"page-header", "breadcrumbs", "empty-state",
		"workspace-grid", "app-table-row", "app-mark", "onboarding-list",
		"detail-layout", "action-rail", "detail-grid-wide",
		"form-workspace", "form-actions-sticky", "upload-label",
		"review-workspace", "review-summary", "review-card", "danger-disclosure",
		"admin-workspace", "lifecycle-list", "admin-stat-emphasis",
		"secret-workspace", "secret-value", "stats-grid-compact",
		"status-badge", "portal-footer",
	} {
		if !strings.Contains(css, "."+class) {
			t.Fatalf("portal.css missing rule for .%s", class)
		}
	}
}
