package session

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type HTTPHandler struct {
	store        Store
	cookieName   string
	cookieSecure bool
	now          func() time.Time
}

func NewHTTPHandler(store Store, cookieName string, secure bool) *HTTPHandler {
	return &HTTPHandler{store: store, cookieName: cookieName, cookieSecure: secure, now: time.Now}
}

func (h *HTTPHandler) Establish(w http.ResponseWriter, r *http.Request, subject string) (Session, error) {
	if h == nil || h.store == nil || h.cookieName == "" {
		return Session{}, errors.New("session HTTP handler is not initialized")
	}
	session, err := h.store.Create(r.Context(), subject, h.now().UTC())
	if err != nil {
		return Session{}, err
	}
	maxAge := int(session.ExpiresAt.Sub(h.now().UTC()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, NewCookie(h.cookieName, session.ID, maxAge, h.cookieSecure))
	return session, nil
}

func (h *HTTPHandler) Current(r *http.Request) (Session, error) {
	if h == nil || h.store == nil || h.cookieName == "" {
		return Session{}, errors.New("session HTTP handler is not initialized")
	}
	cookie, err := r.Cookie(h.cookieName)
	if err != nil {
		return Session{}, ErrNotFound
	}
	return h.store.Touch(r.Context(), cookie.Value, h.now().UTC())
}

func (h *HTTPHandler) SessionID(r *http.Request) (string, error) {
	if h == nil || h.cookieName == "" {
		return "", ErrNotFound
	}
	cookie, err := r.Cookie(h.cookieName)
	if err != nil || cookie.Value == "" {
		return "", ErrNotFound
	}
	return cookie.Value, nil
}

func (h *HTTPHandler) Logout(w http.ResponseWriter, r *http.Request) error {
	if h == nil || h.store == nil || h.cookieName == "" {
		return errors.New("session HTTP handler is not initialized")
	}
	var deleteErr error
	if cookie, err := r.Cookie(h.cookieName); err == nil {
		deleteErr = h.store.Delete(context.WithoutCancel(r.Context()), cookie.Value)
	}
	http.SetCookie(w, ClearCookie(h.cookieName, h.cookieSecure))
	return deleteErr
}
