package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

func NewDiscoveryHandler(issuer string) http.Handler {
	issuer = strings.TrimRight(issuer, "/")
	document := map[string]any{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + "/oauth2/auth",
		"token_endpoint":                        issuer + "/oauth2/token",
		"revocation_endpoint":                   issuer + "/oauth2/revoke",
		"userinfo_endpoint":                     issuer + "/userinfo",
		"jwks_uri":                              issuer + "/.well-known/jwks.json",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"subject_types_supported":               []string{"public"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		"scopes_supported":                      []string{"openid", "profile", "community", "offline_access"},
		"claims_supported":                      []string{"sub", "preferred_username", "name", "picture", "trust_level", "active", "silenced"},
		"code_challenge_methods_supported":      []string{"S256"},
		"userinfo_signing_alg_values_supported": []string{"none"},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeUserInfoError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(document)
	})
}

func NewJWKSProxy(upstream string, client *http.Client) (http.Handler, error) {
	return newPublicProxy(upstream, client, "/.well-known/jwks.json")
}

func NewHydraPublicProxy(upstream string, client *http.Client) (http.Handler, error) {
	return newPublicProxy(upstream, client, "")
}

func newPublicProxy(upstream string, client *http.Client, requiredPath string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimRight(upstream, "/"))
	if err != nil || target.Scheme == "" || target.Host == "" || target.User != nil {
		return nil, errors.New("Hydra public URL must be absolute")
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	if client != nil {
		proxy.Transport = client.Transport
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, POST, HEAD")
			writeUserInfoError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		if requiredPath != "" && r.URL.Path != requiredPath {
			writeUserInfoError(w, http.StatusNotFound, "not_found")
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}
