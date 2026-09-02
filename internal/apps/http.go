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
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/session"
	"connect.xai.run/internal/web"
)

type AssetStore interface {
	Save(context.Context, string, []byte, string) (string, error)
}

type pageRenderer interface {
	Render(http.ResponseWriter, string, any) error
	RenderStatus(http.ResponseWriter, int, string, any) error
}

type HTTPDependencies struct {
	Service       *Service
	Status        identity.StatusLookup
	LevelProgress identity.LevelProgressLookup
	Sessions      *session.HTTPHandler
	CSRF          *session.CSRF
	Renderer      pageRenderer
	Assets        AssetStore
}

type HTTPHandler struct {
	service       *Service
	status        identity.StatusLookup
	levelProgress identity.LevelProgressLookup
	sessions      *session.HTTPHandler
	csrf          *session.CSRF
	renderer      pageRenderer
	assets        AssetStore
}

func NewHTTPHandler(deps HTTPDependencies) *HTTPHandler {
	return &HTTPHandler{
		service: deps.Service, status: deps.Status, levelProgress: deps.LevelProgress, sessions: deps.Sessions, csrf: deps.CSRF,
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
	w.Header().Set("Cache-Control", "no-store")
	if h.service == nil {
		h.fail(w, http.StatusServiceUnavailable, "服务未配置", "应用服务未配置")
		return
	}
	apps, err := h.service.ListMine(r.Context(), subject)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "无法读取应用", err.Error())
		return
	}
	var levelProgress *identity.LevelProgressSnapshot
	if h.levelProgress != nil {
		if snapshot, err := h.levelProgress.CurrentLevelProgress(r.Context(), subject); err == nil {
			levelProgress = &snapshot
		}
	}
	h.render(w, "app-list", map[string]any{
		"Apps": apps, "PageTitle": "应用总览", "Layout": h.layoutFor(r, subject, "apps"), "LevelProgress": levelProgress,
	})
}

func (h *HTTPHandler) newForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	subject, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	data := h.formData(r, "创建应用", "/connect/apps")
	data["PageTitle"] = "创建应用"
	data["Layout"] = h.layoutFor(r, subject, "new")
	h.render(w, "app-form", data)
}

func (h *HTTPHandler) editForm(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.service == nil {
		h.fail(w, http.StatusServiceUnavailable, "服务未配置", "应用服务未配置")
		return
	}
	app, err := h.service.GetMine(r.Context(), subject, id)
	if err != nil {
		h.fail(w, http.StatusNotFound, "应用不存在", "无法读取应用")
		return
	}
	if !editableStatus(app.Status) {
		h.fail(w, http.StatusBadRequest, "应用不可编辑", "当前状态下不能修改应用资料")
		return
	}
	data := h.formData(r, "编辑应用", "/connect/apps/"+string(id)+"/edit")
	data["PageTitle"] = "编辑应用"
	data["Name"] = app.Name
	data["Description"] = app.Description
	data["Callbacks"] = strings.Join(app.CallbackURLs, "\n")
	data["Domains"] = strings.Join(app.VerifiedDomains, "\n")
	data["LogoURL"] = app.LogoURL
	data["Layout"] = h.layoutFor(r, subject, "new")
	h.render(w, "app-form", data)
}

func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	subject, ok := h.authenticated(w, r)
	if !ok {
		return
	}
	if h.service == nil {
		h.fail(w, http.StatusServiceUnavailable, "服务未配置", "应用服务未配置")
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
	if h.service == nil {
		h.fail(w, http.StatusServiceUnavailable, "服务未配置", "应用服务未配置")
		return
	}
	id := domain.ApplicationID(parts[0])
	switch {
	case len(parts) == 2 && parts[1] == "edit" && r.Method == http.MethodGet:
		h.editForm(w, r, subject, id)
	case len(parts) == 2 && parts[1] == "edit" && r.Method == http.MethodPost:
		h.update(w, r, subject, id)
	case r.Method == http.MethodGet && len(parts) == 1:
		w.Header().Set("Cache-Control", "no-store")
		app, err := h.service.GetMine(r.Context(), subject, id)
		if err != nil {
			h.fail(w, http.StatusNotFound, "应用不存在", "无法读取应用")
			return
		}
		h.render(w, "app-list", map[string]any{
			"Apps": []domain.Application{app}, "PageTitle": "应用详情",
			"Layout":      h.layoutFor(r, subject, "apps"),
			"ConfirmedAt": h.confirmationTimestamp(),
		})
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
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "delete":
		h.delete(w, r, subject, id)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *HTTPHandler) update(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	if !h.validCSRF(r) {
		h.fail(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	if err := r.ParseMultipartForm(600 << 10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		h.fail(w, http.StatusBadRequest, "表单无效", "无法解析表单")
		return
	}
	app, err := h.service.GetMine(r.Context(), subject, id)
	if err != nil {
		h.fail(w, http.StatusNotFound, "应用不存在", "无法读取应用")
		return
	}
	input := DraftInput{
		Name:            r.FormValue("name"),
		Description:     r.FormValue("description"),
		CallbackURLs:    splitLines(r.FormValue("callbacks")),
		VerifiedDomains: splitLines(r.FormValue("domains")),
		LogoURL:         app.LogoURL,
	}
	if h.assets != nil {
		if file, header, fileErr := r.FormFile("logo"); fileErr == nil {
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
	if _, err := h.service.UpdateDraft(r.Context(), subject, id, input); err != nil {
		h.fail(w, http.StatusBadRequest, "应用未保存", err.Error())
		return
	}
	http.Redirect(w, r, "/connect/apps/"+string(id), http.StatusSeeOther)
}

func (h *HTTPHandler) viewSecret(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	w.Header().Set("Cache-Control", "no-store")
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
	h.render(w, "secret", map[string]any{
		"Secret": secret, "ID": id, "PageTitle": "Client Secret",
		"Layout": h.layoutFor(r, subject, "apps"),
	})
}

func (h *HTTPHandler) rotateSecret(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	w.Header().Set("Cache-Control", "no-store")
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

func (h *HTTPHandler) delete(w http.ResponseWriter, r *http.Request, subject string, id domain.ApplicationID) {
	w.Header().Set("Cache-Control", "no-store")
	if !h.validCSRF(r) {
		h.fail(w, http.StatusForbidden, "请求已过期", "CSRF 校验失败")
		return
	}
	if err := h.service.Delete(r.Context(), subject, id); err != nil {
		h.fail(w, http.StatusBadRequest, "应用未删除", err.Error())
		return
	}
	http.Redirect(w, r, "/connect/apps", http.StatusSeeOther)
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
	token := ""
	if h.sessions != nil && h.csrf != nil {
		sid, _ := h.sessions.SessionID(r)
		token, _ = h.csrf.Token(sid)
	}
	return map[string]any{"Title": title, "Action": action, "CSRFToken": token}
}

func (h *HTTPHandler) confirmationTimestamp() int64 {
	if h != nil && h.service != nil && h.service.now != nil {
		return h.service.now().UTC().Unix()
	}
	return time.Now().UTC().Unix()
}

func (h *HTTPHandler) layoutFor(r *http.Request, subject, active string) web.Layout {
	layout := web.Layout{Active: active, Subject: subject}
	if h != nil && h.csrf != nil && h.sessions != nil {
		if sessionID, err := h.sessions.SessionID(r); err == nil {
			layout.CSRFToken, _ = h.csrf.Token(sessionID)
		}
	}
	if h != nil && h.status != nil && strings.TrimSpace(subject) != "" {
		if snapshot, err := h.status.CurrentStatus(r.Context(), subject); err == nil {
			layout.DisplayName = snapshot.Name
			layout.Username = snapshot.Username
			layout.AvatarURL = snapshot.AvatarURL
			layout.TrustLevel = snapshot.TrustLevel
			layout.IsReviewer = snapshot.Reviewer || snapshot.Admin
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

func editableStatus(status domain.ApplicationStatus) bool {
	return editableApplicationStatus(status)
}
