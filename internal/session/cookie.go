package session

import (
	"net/http"
	"time"
)

func NewCookie(name, value string, maxAge int, secure bool) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	}
}

func ClearCookie(name string, secure bool) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(0, 0).UTC(), HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode,
	}
}
