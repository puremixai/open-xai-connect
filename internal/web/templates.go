package web

import (
	"embed"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
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
		"canEdit":     canEdit,
		"formatTime":  formatTime,
		"truncate":    truncate,
		"appInitial":  appInitial,
		"assetURL":    AssetURL,
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

func canEdit(status domain.ApplicationStatus) bool {
	return canSubmit(status) || status == domain.StatusApproved
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
	return r.renderStatus(w, status, name, data, nil)
}

// RenderWithFormAction renders a page while allowing the consent page's form
// submission to follow redirects to the approved client's callback origins.
// Other page names intentionally keep the default same-origin form policy.
func (r *Renderer) RenderWithFormAction(w http.ResponseWriter, name string, data any, redirectURIs []string) error {
	return r.renderStatus(w, http.StatusOK, name, data, redirectURIs)
}

func (r *Renderer) renderStatus(w http.ResponseWriter, status int, name string, data any, redirectURIs []string) error {
	if r == nil || r.templates == nil {
		return errors.New("template renderer is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("template name is required")
	}
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy(name, redirectURIs))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	return r.templates.ExecuteTemplate(w, name+".html", data)
}

func contentSecurityPolicy(pageName string, redirectURIs []string) string {
	formAction := []string{"'self'"}
	if pageName == "consent" {
		formAction = append(formAction, formActionSources(redirectURIs)...)
	}
	return "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action " + strings.Join(formAction, " ")
}

func formActionSources(redirectURIs []string) []string {
	seen := make(map[string]struct{}, len(redirectURIs))
	sources := make([]string, 0, len(redirectURIs))
	for _, raw := range redirectURIs {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.RawQuery != "" {
			continue
		}
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if host == "" || strings.ContainsAny(host, " \t\r\n") || strings.Contains(host, "*") || strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil {
			continue
		}
		port := parsed.Port()
		if port != "" {
			if _, err := strconv.Atoi(port); err != nil {
				continue
			}
		}
		origin := "https://" + host
		if port != "" && port != "443" {
			origin += ":" + port
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		sources = append(sources, origin)
	}
	sort.Strings(sources)
	return sources
}
