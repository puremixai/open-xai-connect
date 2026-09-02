package web

import "net/http"

// DocsPath is the public, same-origin entry point for Connect documentation.
const DocsPath = "/docs"

// RegisterDocsRoutes serves the public documentation page without requiring a
// Portal session. Documentation links appear throughout authenticated pages,
// but the integration guide is useful before an application is created.
func RegisterDocsRoutes(mux *http.ServeMux, renderer *Renderer) {
	if mux == nil {
		return
	}
	mux.HandleFunc(DocsPath, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DocsPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if renderer == nil {
			http.Error(w, "documentation is not configured", http.StatusServiceUnavailable)
			return
		}
		if err := renderer.Render(w, "docs", map[string]any{"PageTitle": "文档"}); err != nil {
			// Render writes the response headers before executing the template;
			// there is no safe second response to send after an execution error.
			return
		}
	})
}
