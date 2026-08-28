package apps

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/web"
)

type AssetStore interface {
	Save(context.Context, string, []byte, string) (string, error)
}

type HTTPDependencies struct {
	Service  *Service
	Sessions *session.HTTPHandler
	CSRF     *session.CSRF
	Renderer *web.Renderer
	Assets   AssetStore
}

type HTTPHandler struct {
	service  *Service
	sessions *session.HTTPHandler
	csrf     *session.CSRF
	renderer *web.Renderer
	assets   AssetStore
}

func NewHTTPHandler(deps HTTPDependencies) *HTTPHandler {
	return &HTTPHandler{
		service: deps.Service, sessions: deps.Sessions, csrf: deps.CSRF,
		renderer: deps.Renderer, assets: deps.Assets,
	}
}

func RegisterRoutes(mux *http.ServeMux, handler *HTTPHandler) {
	mux.HandleFunc("/connect/apps", handler.listOrCreate)
	mux.HandleFunc("/connect/apps/new", handler.newForm)
	mux.HandleFunc("/connect/apps/", handler.item)
}

func (h *HTTPHandler) listOrCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.list(w, r)
		return
	}
	if r.Method == http.MethodPost {
		h.create(w, r)
		return
	}
	w.Header().Set("Allow", "GET, POST")
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	apps, err := h.service.ListMine(r.Context(), subject)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "无法读取应用", err.Error())
		return
	}
	h.render(w, "app-list", map[string]any{"Apps": apps})
}

func (h *HTTPHandler) newForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	_, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	h.render(w, "app-form", h.formData(r, "创建应用", "/connect/apps"))
}

func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if !h.validCSRF(r) {
		h.fail(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	if err := r.ParseMultipartForm(600 << 10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		h.fail(w, http.StatusBadRequest, "表单无效", "无法解析表单")
		return
	}
	input := DraftInput{
		Name: r.FormValue("name"), Description: r.FormValue("description"),
		CallbackURLs:    splitLines(r.FormValue("callbacks")),
		VerifiedDomains: splitLines(r.FormValue("domains")),
	}
	if h.assets != nil {
		if file, header, err := r.FormFile("logo"); err == nil {
			defer file.Close()
			data, readErr := io.ReadAll(io.LimitReader(file, 600<<10))
			if readErr != nil {
				h.fail(w, http.StatusBadRequest, "Logo 无效", "无法读取 Logo")
				return
			}
			logoURL, saveErr := h.assets.Save(r.Context(), subject, data, header.Header.Get("Content-Type"))
			if saveErr != nil {
				h.fail(w, http.StatusBadRequest, "Logo 无效", saveErr.Error())
				return
			}
			input.LogoURL = logoURL
		}
	}
	app, err := h.service.CreateDraft(r.Context(), subject, input)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "应用未创建", err.Error())
		return
	}
	http.Redirect(w, r, "/connect/apps/"+string(app.ID), http.StatusSeeOther)
}

func (h *HTTPHandler) item(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/connect/apps/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	subject, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	id := domain.ApplicationID(parts[0])
	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		app, err := h.service.GetMine(r.Context(), subject, id)
		if err != nil {
			h.fail(w, http.StatusNotFound, "应用不存在", "无法读取应用")
			return
		}
		h.render(w, "app-list", map[string]any{"Apps": []domain.Application{app}})
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "submit":
		if !h.validCSRF(r) {
			h.fail(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
			return
		}
		if _, err := h.service.Submit(r.Context(), subject, id); err != nil {
			h.fail(w, http.StatusBadRequest, "提交失败", err.Error())
			return
		}
		http.Redirect(w, r, "/connect/apps/"+string(id), http.StatusSeeOther)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "secret":
		h.viewSecret(w, r, subject, id)
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "rotate-secret":
		h.rotateSecret(w, r, subject, id)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) viewSecret(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	if !h.validCSRF(r) {
		h.fail(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	confirmedAt, err := parseConfirmation(r.FormValue("confirmed_at"))
	if err != nil {
		h.fail(w, http.StatusForbidden, "需要重新确认身份", "敏感操作确认已失效")
		return
	}
	secret, err := h.service.ViewSecret(r.Context(), subject, id, confirmedAt)
	if err != nil {
		h.fail(w, http.StatusForbidden, "无法查看 Secret", err.Error())
		return
	}
	h.render(w, "secret", map[string]any{"Secret": secret, "ID": id})
}

func (h *HTTPHandler) rotateSecret(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	if !h.validCSRF(r) {
		h.fail(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	confirmedAt, err := parseConfirmation(r.FormValue("confirmed_at"))
	if err != nil {
		h.fail(w, http.StatusForbidden, "需要重新确认身份", "敏感操作确认已失效")
		return
	}
	if _, err := h.service.RotateSecret(r.Context(), subject, id, confirmedAt); err != nil {
		h.fail(w, http.StatusForbidden, "无法轮换 Secret", err.Error())
		return
	}
	http.Redirect(w, r, "/connect/apps/"+string(id), http.StatusSeeOther)
}

func (h *HTTPHandler) authenticated(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.sessions == nil {
		h.fail(w, http.StatusServiceUnavailable, "服务未配置", "Session 服务未配置")
		return "", false
	}
	current, err := h.sessions.Current(r)
	if err != nil {
		http.Redirect(w, r, "/connect/login?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return "", false
	}
	return current.UserSubject, true
}

func (h *HTTPHandler) validCSRF(r *http.Request) bool {
	if h == nil || h.csrf == nil || h.sessions == nil {
		return false
	}
	sid, err := h.sessions.SessionID(r)
	return err == nil && h.csrf.Verify(sid, r.FormValue("csrf_token"))
}

func (h *HTTPHandler) formData(r *http.Request, title, action string) map[string]any {
	sid, _ := h.sessions.SessionID(r)
	token, _ := h.csrf.Token(sid)
	return map[string]any{"Title": title, "Action": action, "CSRFToken": token}
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

func (h *HTTPHandler) fail(w http.ResponseWriter, status int, title, message string) {
	if h.renderer == nil {
		http.Error(w, message, status)
		return
	}
	if err := h.renderer.RenderStatus(w, status, "error", map[string]any{"Title": title, "Message": message, "Back": "/connect/apps"}); err != nil {
		http.Error(w, message, status)
	}
}

func splitLines(value string) []string {
	lines := strings.Split(value, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if value := strings.TrimSpace(line); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func parseConfirmation(value string) (time.Time, error) {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0).UTC(), nil
}
