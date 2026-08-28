package reviews

import (
	"net/http"
	"strings"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/web"
)

type HTTPDependencies struct {
	Service  *Service
	Sessions *session.HTTPHandler
	CSRF     *session.CSRF
	Renderer *web.Renderer
}

type HTTPHandler struct {
	service  *Service
	sessions *session.HTTPHandler
	csrf     *session.CSRF
	renderer *web.Renderer
}

func NewHTTPHandler(deps HTTPDependencies) *HTTPHandler {
	return &HTTPHandler{service: deps.Service, sessions: deps.Sessions, csrf: deps.CSRF, renderer: deps.Renderer}
}

func RegisterRoutes(mux *http.ServeMux, handler *HTTPHandler) {
	mux.HandleFunc("/connect/review", handler.list)
	mux.HandleFunc("/connect/review/", handler.decide)
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
	h.render(w, "review-queue", map[string]any{"Apps": apps, "CSRFToken": token})
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
