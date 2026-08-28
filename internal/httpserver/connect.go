package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"connect.xai.run/internal/session"
)

type LoginBeginFunc func(context.Context, string) (string, error)
type LoginCompleteFunc func(context.Context, *http.Request) (subject string, redirectTo string, err error)

type ConnectDependencies struct {
	Sessions          *session.HTTPHandler
	CSRF              *session.CSRF
	BeginLogin        LoginBeginFunc
	BeginSessionLogin LoginBeginFunc
	CompleteLogin     LoginCompleteFunc
}

func RegisterConnectRoutes(mux *http.ServeMux, deps ConnectDependencies) {
	mux.HandleFunc("/connect/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		challenge := strings.TrimSpace(r.URL.Query().Get("login_challenge"))
		returnTo := strings.TrimSpace(r.URL.Query().Get("return_to"))
		if challenge == "" && deps.BeginSessionLogin == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "login_challenge is required"})
			return
		}
		if challenge != "" && deps.BeginLogin == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "login flow is not configured"})
			return
		}
		if challenge == "" {
			if err := validateRelativeRedirect(returnTo); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "return_to must be a relative path"})
				return
			}
		}
		begin := deps.BeginLogin
		argument := challenge
		if challenge == "" {
			begin = deps.BeginSessionLogin
			argument = returnTo
		}
		location, err := begin(r.Context(), argument)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "identity provider unavailable"})
			return
		}
		if err := validateAbsoluteHTTPS(location); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid identity provider redirect"})
			return
		}
		http.Redirect(w, r, location, http.StatusFound)
	})

	mux.HandleFunc("/connect/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || deps.Sessions == nil || deps.CompleteLogin == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "login callback is not configured"})
			return
		}
		subject, redirectTo, err := deps.CompleteLogin(r.Context(), r)
		if err != nil || strings.TrimSpace(subject) == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "login could not be verified"})
			return
		}
		if err := validateCallbackRedirect(redirectTo); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid callback redirect"})
			return
		}
		if _, err := deps.Sessions.Establish(w, r, subject); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session unavailable"})
			return
		}
		http.Redirect(w, r, redirectTo, http.StatusSeeOther)
	})

	mux.HandleFunc("/connect/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || deps.Sessions == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		current, err := deps.Sessions.Current(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"subject": current.UserSubject})
	})

	mux.HandleFunc("/connect/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || deps.Sessions == nil {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if deps.CSRF != nil {
			sessionID, sessionErr := deps.Sessions.SessionID(r)
			if sessionErr != nil || !deps.CSRF.Verify(sessionID, r.FormValue("csrf_token")) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf validation failed"})
				return
			}
		}
		if err := deps.Sessions.Logout(w, r); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session store unavailable"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func validateAbsoluteHTTPS(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("redirect must be absolute HTTPS URL")
	}
	return nil
}

func validateRelativeRedirect(raw string) error {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return errors.New("redirect must be a relative path")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return errors.New("redirect must be a relative path")
	}
	return nil
}

func validateCallbackRedirect(raw string) error {
	if err := validateRelativeRedirect(raw); err == nil {
		return nil
	}
	return validateAbsoluteHTTPS(raw)
}
