package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/identity"
)

//go:embed templates/*.html
var templateFS embed.FS

type Renderer struct {
	templates *template.Template
}

func NewRenderer() (*Renderer, error) {
	parsed, err := template.New("connect").Funcs(template.FuncMap{
		"statusLabel":            statusLabel,
		"statusTone":             statusTone,
		"countStatus":            countStatus,
		"countOpen":              countOpen,
		"add":                    add,
		"canSubmit":              canSubmit,
		"canEdit":                canEdit,
		"canDelete":              canDelete,
		"formatTime":             formatTime,
		"truncate":               truncate,
		"appInitial":             appInitial,
		"assetURL":               AssetURL,
		"levelRequirementGroups": levelRequirementGroups,
		"levelGroupLabel":        levelGroupLabel,
		"requirementOperator":    requirementOperator,
		"requirementUnit":        requirementUnit,
		"requirementStatusLabel": requirementStatusLabel,
		"blockingStatusLabel":    blockingStatusLabel,
		"promotionModeLabel":     promotionModeLabel,
		"requirementScopeHint":   requirementScopeHint,
		"requirementLabelID":     requirementLabelID,
		"requirementsMet":        requirementsMet,
		"progressValue":          progressValue,
		"progressMax":            progressMax,
	}).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Renderer{templates: parsed}, nil
}

type levelRequirementGroup struct {
	Key          string
	Requirements []identity.LevelRequirement
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

func canDelete(status domain.ApplicationStatus) bool {
	switch status {
	case domain.StatusDraft, domain.StatusPendingReview, domain.StatusApproved, domain.StatusRejected, domain.StatusChangesRequested, domain.StatusRevoked:
		return true
	default:
		return false
	}
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

func levelRequirementGroups(requirements []identity.LevelRequirement) []levelRequirementGroup {
	groups := make([]levelRequirementGroup, 0, len(requirements))
	indexByKey := make(map[string]int, len(requirements))
	for _, requirement := range requirements {
		key := strings.TrimSpace(requirement.Group)
		if key == "" {
			key = "other"
		}
		if index, ok := indexByKey[key]; ok {
			groups[index].Requirements = append(groups[index].Requirements, requirement)
			continue
		}
		indexByKey[key] = len(groups)
		groups = append(groups, levelRequirementGroup{
			Key:          key,
			Requirements: []identity.LevelRequirement{requirement},
		})
	}
	return groups
}

func levelGroupLabel(group string) string {
	switch strings.TrimSpace(group) {
	case "activity":
		return "活跃程度"
	case "interaction":
		return "互动参与"
	case "compliance":
		return "合规与账号状态"
	case "", "other":
		return "其他条件"
	default:
		return "其他条件"
	}
}

func requirementOperator(operator string) string {
	switch strings.TrimSpace(operator) {
	case "at_least":
		return "至少"
	case "at_most":
		return "不超过"
	case "equals":
		return "等于"
	default:
		return operator
	}
}

func requirementUnit(unit string) string {
	switch strings.TrimSpace(unit) {
	case "count":
		return "项"
	case "days":
		return "天"
	case "minutes":
		return "分钟"
	default:
		return unit
	}
}

func requirementStatusLabel(met bool) string {
	if met {
		return "已达成"
	}
	return "未达成"
}

func blockingStatusLabel(met bool) string {
	if met {
		return "已满足"
	}
	return "未满足"
}

func promotionModeLabel(mode string) string {
	switch strings.TrimSpace(mode) {
	case "automatic":
		return "自动升级"
	case "manual":
		return "管理员授予"
	case "locked":
		return "等级锁定"
	case "none":
		return "已满级"
	default:
		return mode
	}
}

func requirementScopeHint(requirement identity.LevelRequirement) string {
	switch requirement.Scope {
	case "rolling_period":
		if requirement.PeriodDays != nil {
			return fmt.Sprintf("按最近 %d 天数据计算", *requirement.PeriodDays)
		}
		return "按最近周期数据计算"
	case "account_lifetime":
		return "按账号累计数据计算"
	case "all_time":
		return "按全站累计数据计算"
	default:
		return ""
	}
}

func requirementLabelID(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	if key == "" {
		key = "other"
	}
	var builder strings.Builder
	builder.WriteString("level-requirement-")
	lastHyphen := false
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastHyphen = false
		case r == '-', r == '_', r == ' ':
			if !lastHyphen {
				builder.WriteByte('-')
				lastHyphen = true
			}
		default:
			if !lastHyphen {
				builder.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	id := strings.Trim(builder.String(), "-")
	if id == "level-requirement" {
		id = "level-requirement-other"
	}
	return id + "-label"
}

func requirementsMet(value *bool) bool {
	return value != nil && *value
}

func progressValue(current, target int64) int64 {
	if target <= 0 || current <= 0 {
		return 0
	}
	if current > target {
		return target
	}
	return current
}

func progressMax(target int64) int64 {
	if target <= 0 {
		return 1
	}
	return target
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
	scriptSource := "'self'"
	turnstileSources := ""
	if pageName == "home-verification" {
		scriptSource += " https://challenges.cloudflare.com"
		turnstileSources = "; frame-src 'self' https://challenges.cloudflare.com; connect-src 'self' https://challenges.cloudflare.com"
	}
	return "default-src 'self'; script-src " + scriptSource + "; style-src 'self'; img-src 'self' data:" + turnstileSources + "; frame-ancestors 'none'; base-uri 'self'; form-action " + strings.Join(formAction, " ")
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
