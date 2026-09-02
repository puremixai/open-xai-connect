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

func TestFingerprintedStylesheetURLsAreImmutable(t *testing.T) {
	mux := http.NewServeMux()
	RegisterAssetRoutes(mux)

	url := AssetURL("portal.css")
	if url == AssetsPath+"/portal.css" || !strings.HasPrefix(url, AssetsPath+"/portal.") {
		t.Fatalf("AssetURL() = %q, want content-addressed path", url)
	}

	fingerprinted := httptest.NewRecorder()
	mux.ServeHTTP(fingerprinted, httptest.NewRequest(http.MethodGet, url, nil))
	if fingerprinted.Code != http.StatusOK {
		t.Fatalf("fingerprinted status = %d", fingerprinted.Code)
	}
	if cache := fingerprinted.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
		t.Fatalf("fingerprinted Cache-Control = %q, want immutable", cache)
	}

	legacy := httptest.NewRecorder()
	mux.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, AssetsPath+"/portal.css", nil))
	if legacy.Code != http.StatusOK {
		t.Fatalf("legacy alias status = %d", legacy.Code)
	}
	if legacy.Body.String() != fingerprinted.Body.String() {
		t.Fatal("legacy alias serves different content than the fingerprinted URL")
	}
	if cache := legacy.Header().Get("Cache-Control"); strings.Contains(cache, "immutable") {
		t.Fatalf("legacy alias Cache-Control = %q, want short lifetime", cache)
	}

	unknown := httptest.NewRecorder()
	mux.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, AssetsPath+"/portal.deadbeef.css", nil))
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown fingerprint status = %d, want 404", unknown.Code)
	}
}

func TestPortalStylesheetCoversClassesUsedByTemplates(t *testing.T) {
	stylesheet, err := staticFS.ReadFile("static/portal.css")
	if err != nil {
		t.Fatalf("read portal.css: %v", err)
	}
	css := string(stylesheet)
	for _, class := range []string{
		"portal-topbar", "portal-brand", "portal-shell", "portal-sidebar", "portal-nav", "portal-main",
		"page-header", "breadcrumbs", "empty-state",
		"workspace-grid", "app-table-row", "app-mark", "onboarding-list",
		"detail-layout", "action-rail", "detail-grid-wide",
		"form-page", "form-workspace", "form-actions-sticky", "upload-label", "upload-thumb", "form-footnote",
		"review-workspace", "review-summary", "review-card", "danger-actions", "danger-action", "danger-action-summary", "danger-action-body", "danger-panel",
		"admin-workspace", "lifecycle-list", "admin-stat-emphasis",
		"secret-workspace", "secret-value", "stats-grid-compact",
		"status-badge", "portal-footer",
	} {
		if !strings.Contains(css, "."+class) {
			t.Fatalf("portal.css missing rule for .%s", class)
		}
	}
}

func TestPortalStylesInsetApplicationDetailSteps(t *testing.T) {
	stylesheet, err := staticFS.ReadFile("static/portal.css")
	if err != nil {
		t.Fatalf("read portal.css: %v", err)
	}
	if !strings.Contains(string(stylesheet), ".detail-steps { display: grid; margin: 0; padding: var(--space-1) 20px var(--space-4);") {
		t.Fatal("application detail steps should align with the panel content inset")
	}
}

func TestPortalFormSeparatesAuthorizationSectionFromLogo(t *testing.T) {
	stylesheet, err := staticFS.ReadFile("static/portal.css")
	if err != nil {
		t.Fatalf("read portal.css: %v", err)
	}
	if !strings.Contains(string(stylesheet), ".form-section + .form-section { margin-top: var(--space-6);") {
		t.Fatal("authorization section should have a full spacing token before its title")
	}
}
