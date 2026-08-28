package web

import (
	"embed"
	"errors"
	"html/template"
	"net/http"
	"strings"
)

//go:embed templates/*.html
var templateFS embed.FS

type Renderer struct {
	templates *template.Template
}

func NewRenderer() (*Renderer, error) {
	parsed, err := template.New("connect").ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{templates: parsed}, nil
}

func (r *Renderer) Render(w http.ResponseWriter, name string, data any) error {
	return r.RenderStatus(w, http.StatusOK, name, data)
}

func (r *Renderer) RenderStatus(w http.ResponseWriter, status int, name string, data any) error {
	if r == nil || r.templates == nil {
		return errors.New("template renderer is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("template name is required")
	}
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return r.templates.ExecuteTemplate(w, name+".html", data)
}
