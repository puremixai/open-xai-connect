package httpserver

import "net/http"

type OAuthDependencies struct {
	Discovery http.Handler
	JWKS      http.Handler
	Hydra     http.Handler
	UserInfo  http.Handler
}

func RegisterOAuthRoutes(mux *http.ServeMux, deps OAuthDependencies) {
	mountOrUnavailable := func(path string, handler http.Handler) {
		if handler == nil {
			handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "endpoint unavailable"})
			})
		}
		mux.Handle(path, handler)
	}
	mountOrUnavailable("/.well-known/openid-configuration", deps.Discovery)
	mountOrUnavailable("/.well-known/jwks.json", deps.JWKS)
	mountOrUnavailable("/userinfo", deps.UserInfo)
	mountOrUnavailable("/oauth2/", deps.Hydra)
}
