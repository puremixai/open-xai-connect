package web

import (
	"embed"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
)

//go:embed templates/*.html
var templateFS embed.FS

type Renderer struct {
	templates *template.Template
}

func NewRenderer() (*Renderer, error) {
	parsed, err := template.New("connect").Funcs(template.FuncMap{
		"statusLabel": statusLabel,
		"statusTone":  statusTone,
		"countStatus": countStatus,
		"countOpen":   countOpen,
		"add":         add,
		"canSubmit":   canSubmit,
		"formatTime":  formatTime,
		"truncate":    truncate,
		"appInitial":  appInitial,
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{templates: parsed}, nil
}

func statusLabel(status domain.ApplicationStatus) string {
	switch status {
	case domain.StatusDraft:
		return "草稿"
	case domain.StatusPendingReview:
		return "待审核"
	case domain.StatusProvisioning:
		return "正在上线"
	case domain.StatusApproved:
		return "已上线"
	case domain.StatusRejected:
		return "已驳回"
	case domain.StatusChangesRequested:
		return "需要修改"
	case domain.StatusRevoked:
		return "已撤销"
	default:
		return "未知状态"
	}
}

func statusTone(status domain.ApplicationStatus) string {
	switch status {
	case domain.StatusApproved:
		return "success"
	case domain.StatusPendingReview, domain.StatusProvisioning:
		return "progress"
	case domain.StatusRejected, domain.StatusRevoked:
		return "danger"
	case domain.StatusChangesRequested:
		return "warning"
	default:
		return "neutral"
	}
}

func countStatus(apps []domain.Application, wanted string) int {
	count := 0
	for _, app := range apps {
		if string(app.Status) == wanted {
			count++
		}
	}
	return count
}

func countOpen(apps []domain.Application) int {
	count := 0
	for _, app := range apps {
		if app.IsOpen() {
			count++
		}
	}
	return count
}

func add(left, right int) int {
	return left + right
}

func canSubmit(status domain.ApplicationStatus) bool {
	return status == domain.StatusDraft || status == domain.StatusChangesRequested || status == domain.StatusRejected
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func appInitial(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) == 0 {
		return "A"
	}
	return string(runes[0])
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
