package reviews

import (
	"net/http"
	"strings"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/web"
)

type HTTPDependencies struct {
	Service  *Service
	Status   identity.StatusLookup
	Sessions *session.HTTPHandler
	CSRF     *session.CSRF
	Renderer *web.Renderer
}

type HTTPHandler struct {
	service  *Service
	status   identity.StatusLookup
	sessions *session.HTTPHandler
	csrf     *session.CSRF
	renderer *web.Renderer
}

type AdminStats struct {
	Total, Draft, PendingReview, Provisioning, Approved int
	ChangesRequested, Rejected, Revoked                 int
}

func NewHTTPHandler(deps HTTPDependencies) *HTTPHandler {
	return &HTTPHandler{service: deps.Service, status: deps.Status, sessions: deps.Sessions, csrf: deps.CSRF, renderer: deps.Renderer}
}

func RegisterRoutes(mux *http.ServeMux, handler *HTTPHandler) {
	mux.HandleFunc("/connect/review", handler.list)
	mux.HandleFunc("/connect/review/", handler.decide)
	mux.HandleFunc("/connect/admin", handler.adminOverview)
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	subject, ok := h.currentSubject(w, r)
	if !ok {
		return
	}
	apps, err := h.service.ListPending(r.Context(), subject)
	if err != nil {
		h.errorPage(w, http.StatusForbidden, "无法读取审核队列", err.Error())
		return
	}
	token := h.csrfToken(r)
	h.render(w, "review-queue", map[string]any{
		"Apps": apps, "CSRFToken": token, "PageTitle": "应用审核",
		"Layout": h.layoutFor(r, subject, "review"),
	})
}

func (h *HTTPHandler) decide(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	subject, ok := h.currentSubject(w, r)
	if !ok {
		return
	}
	if !h.validCSRF(r) {
		h.errorPage(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	rawID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/connect/review/"), "/")
	if rawID == "" || strings.Contains(rawID, "/") {
		http.NotFound(w, r)
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	var err error
	switch r.FormValue("decision") {
	case "approve":
		err = h.service.Approve(r.Context(), subject, domain.ApplicationID(rawID), reason)
	case "changes":
		err = h.service.RequestChanges(r.Context(), subject, domain.ApplicationID(rawID), reason)
	case "reject":
		err = h.service.Reject(r.Context(), subject, domain.ApplicationID(rawID), reason)
	default:
		h.errorPage(w, http.StatusBadRequest, "审核决定无效", "请选择一个审核决定")
		return
	}
	if err != nil {
		h.errorPage(w, http.StatusBadRequest, "审核未完成", err.Error())
		return
	}
	http.Redirect(w, r, "/connect/review", http.StatusSeeOther)
}

func (h *HTTPHandler) currentSubject(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.sessions == nil || h.service == nil {
		h.errorPage(w, http.StatusServiceUnavailable, "服务未配置", "审核服务未配置")
		return "", false
	}
	current, err := h.sessions.Current(r)
	if err != nil {
		http.Redirect(w, r, "/connect/login?return_to=%2Fconnect%2Freview", http.StatusFound)
		return "", false
	}
	return current.UserSubject, true
}

func (h *HTTPHandler) validCSRF(r *http.Request) bool {
	if h == nil || h.csrf == nil || h.sessions == nil {
		return false
	}
	id, err := h.sessions.SessionID(r)
	return err == nil && h.csrf.Verify(id, r.FormValue("csrf_token"))
}

func (h *HTTPHandler) csrfToken(r *http.Request) string {
	if h == nil || h.csrf == nil || h.sessions == nil {
		return ""
	}
	id, _ := h.sessions.SessionID(r)
	token, _ := h.csrf.Token(id)
	return token
}

func (h *HTTPHandler) adminOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	subject, ok := h.currentSubject(w, r)
	if !ok {
		return
	}
	if h.status == nil || h.service == nil || h.service.apps == nil {
		h.errorPage(w, http.StatusServiceUnavailable, "无法读取运营概览", "管理员服务未配置")
		return
	}
	status, err := h.status.CurrentStatus(r.Context(), subject)
	if err != nil {
		h.errorPage(w, http.StatusServiceUnavailable, "无法读取运营概览", "无法同步当前管理员身份")
		return
	}
	if !status.Admin {
		h.errorPage(w, http.StatusForbidden, "没有管理员权限", "请联系 xai.run 管理员加入 connect-admins 群组")
		return
	}
	stats := AdminStats{}
	for _, applicationStatus := range []domain.ApplicationStatus{
		domain.StatusDraft, domain.StatusPendingReview, domain.StatusProvisioning,
		domain.StatusApproved, domain.StatusChangesRequested, domain.StatusRejected,
		domain.StatusRevoked,
	} {
		applications, listErr := h.service.apps.ListByStatus(r.Context(), applicationStatus)
		if listErr != nil {
			h.errorPage(w, http.StatusInternalServerError, "无法读取运营概览", "应用统计暂时不可用")
			return
		}
		count := len(applications)
		stats.Total += count
		switch applicationStatus {
		case domain.StatusDraft:
			stats.Draft = count
		case domain.StatusPendingReview:
			stats.PendingReview = count
		case domain.StatusProvisioning:
			stats.Provisioning = count
		case domain.StatusApproved:
			stats.Approved = count
		case domain.StatusChangesRequested:
			stats.ChangesRequested = count
		case domain.StatusRejected:
			stats.Rejected = count
		case domain.StatusRevoked:
			stats.Revoked = count
		}
	}
	h.render(w, "admin-overview", map[string]any{
		"Stats": stats, "PageTitle": "运营概览", "Layout": h.layoutFor(r, subject, "admin"),
	})
}

func (h *HTTPHandler) layoutFor(r *http.Request, subject, active string) web.Layout {
	layout := web.Layout{Active: active, Subject: subject, CSRFToken: h.csrfToken(r)}
	if h != nil && h.status != nil && strings.TrimSpace(subject) != "" {
		if snapshot, err := h.status.CurrentStatus(r.Context(), subject); err == nil {
			layout.DisplayName = snapshot.Name
			layout.Username = snapshot.Username
			layout.AvatarURL = snapshot.AvatarURL
			layout.TrustLevel = snapshot.TrustLevel
			layout.IsReviewer = snapshot.Reviewer
			layout.IsAdmin = snapshot.Admin
		}
	}
	return layout
}

func (h *HTTPHandler) render(w http.ResponseWriter, name string, data any) {
	if h.renderer == nil {
		http.Error(w, "template service unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := h.renderer.Render(w, name, data); err != nil {
		http.Error(w, "template render failed", http.StatusInternalServerError)
	}
}

func (h *HTTPHandler) errorPage(w http.ResponseWriter, status int, title, message string) {
	if h.renderer == nil {
		http.Error(w, message, status)
		return
	}
	_ = h.renderer.RenderStatus(w, status, "error", map[string]any{
		"Title": title, "Message": message, "Back": "/connect/review",
	})
}
