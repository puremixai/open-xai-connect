package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthRoutesMountProtocolHandlers(t *testing.T) {
	marker := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}
	handler := New(Dependencies{
		Register: func(mux *http.ServeMux) {
			RegisterOAuthRoutes(mux, OAuthDependencies{
				Discovery: http.HandlerFunc(marker),
				JWKS:      http.HandlerFunc(marker),
				Hydra:     http.HandlerFunc(marker),
				UserInfo:  http.HandlerFunc(marker),
			})
		},
	})
	for _, path := range []string{"/.well-known/openid-configuration", "/.well-known/jwks.json", "/oauth2/auth", "/userinfo"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusTeapot {
			t.Fatalf("%s status = %d", path, response.Code)
		}
	}
}
