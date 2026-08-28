package oauth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"connect.xai.run/internal/session"
	"connect.xai.run/internal/web"
)

type ConsentHTTPDependencies struct {
	Service  *ConsentService
	Sessions *session.HTTPHandler
	CSRF     *session.CSRF
	Renderer *web.Renderer
}

type ConsentHTTPHandler struct {
	service  *ConsentService
	sessions *session.HTTPHandler
	csrf     *session.CSRF
	renderer *web.Renderer
}

func NewConsentHTTPHandler(deps ConsentHTTPDependencies) *ConsentHTTPHandler {
	return &ConsentHTTPHandler{service: deps.Service, sessions: deps.Sessions, csrf: deps.CSRF, renderer: deps.Renderer}
}

func RegisterConsentRoutes(mux *http.ServeMux, handler *ConsentHTTPHandler) {
	mux.HandleFunc("/connect/consent", handler.handle)
}

func (h *ConsentHTTPHandler) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h == nil || h.service == nil || h.sessions == nil {
		h.errorPage(w, http.StatusServiceUnavailable, "服务未配置", "授权服务未配置")
		return
	}
	challenge := strings.TrimSpace(r.URL.Query().Get("consent_challenge"))
	if challenge == "" {
		h.errorPage(w, http.StatusBadRequest, "授权请求无效", "缺少 consent_challenge")
		return
	}
	current, err := h.sessions.Current(r)
	if err != nil {
		login := "/connect/login?login_challenge=" + url.QueryEscape(strings.TrimSpace(r.URL.Query().Get("login_challenge")))
		if strings.TrimSpace(r.URL.Query().Get("login_challenge")) == "" {
			login = "/connect/login?return_to=" + url.QueryEscape("/connect/consent?consent_challenge="+challenge)
		}
		http.Redirect(w, r, login, http.StatusFound)
		return
	}
	request, err := h.service.Request(r.Context(), challenge)
	if err != nil {
		h.errorPage(w, http.StatusBadRequest, "授权请求无效", "应用或授权请求不可用")
		return
	}
	if request.Subject != "" && request.Subject != current.UserSubject {
		h.errorPage(w, http.StatusForbidden, "授权请求无效", "当前登录用户与授权请求不匹配")
		return
	}
	if r.Method == http.MethodGet {
		h.render(w, "consent", map[string]any{
			"Client": request.Client, "Scopes": request.RequestedScope,
			"Action":    "/connect/consent?consent_challenge=" + url.QueryEscape(challenge),
			"CSRFToken": h.csrfToken(r),
		})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !h.validCSRF(r) {
		h.errorPage(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	decision := strings.TrimSpace(r.FormValue("decision"))
	var redirect string
	if decision == "allow" {
		granted := strings.Fields(r.FormValue("scope"))
		if len(granted) == 0 {
			granted = request.RequestedScope
		}
		redirect, err = h.service.Accept(r.Context(), challenge, current.UserSubject, granted, r.FormValue("remember") == "1")
	} else if decision == "deny" {
		redirect, err = h.service.Reject(r.Context(), challenge, "user_denied")
	} else {
		h.errorPage(w, http.StatusBadRequest, "授权决定无效", "请选择允许或拒绝")
		return
	}
	if err != nil {
		h.errorPage(w, http.StatusBadRequest, "授权未完成", "授权服务暂时不可用")
		return
	}
	if err := validateHTTPSRedirect(redirect); err != nil {
		h.errorPage(w, http.StatusInternalServerError, "授权响应无效", "授权服务返回了无效地址")
		return
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (h *ConsentHTTPHandler) validCSRF(r *http.Request) bool {
	if h == nil || h.csrf == nil {
		return false
	}
	sid, err := h.sessions.SessionID(r)
	return err == nil && h.csrf.Verify(sid, r.FormValue("csrf_token"))
}

func (h *ConsentHTTPHandler) csrfToken(r *http.Request) string {
	if h == nil || h.csrf == nil {
		return ""
	}
	sid, err := h.sessions.SessionID(r)
	if err != nil {
		return ""
	}
	token, _ := h.csrf.Token(sid)
	return token
}

func (h *ConsentHTTPHandler) render(w http.ResponseWriter, name string, data any) {
	if h.renderer == nil {
		http.Error(w, "template service unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := h.renderer.Render(w, name, data); err != nil {
		http.Error(w, "template render failed", http.StatusInternalServerError)
	}
}

func (h *ConsentHTTPHandler) errorPage(w http.ResponseWriter, status int, title, message string) {
	if h == nil || h.renderer == nil {
		http.Error(w, message, status)
		return
	}
	_ = h.renderer.RenderStatus(w, status, "error", map[string]any{
		"Title": title, "Message": message, "Back": "/connect/me",
	})
}

func validateHTTPSRedirect(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("redirect must be an absolute HTTPS URL")
	}
	return nil
}
