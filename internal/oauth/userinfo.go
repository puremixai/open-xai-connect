package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/store"
)

var ErrUnauthorized = errors.New("userinfo authorization failed")

type UserInfoService struct {
	hydra  hydra.Client
	status identity.StatusLookup
	users  store.UserRepository
	apps   store.ApplicationRepository
}

func NewUserInfoService(hydraClient hydra.Client, status identity.StatusLookup, users store.UserRepository, apps store.ApplicationRepository) *UserInfoService {
	return &UserInfoService{hydra: hydraClient, status: status, users: users, apps: apps}
}

func (s *UserInfoService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeUserInfoError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	token, err := bearerToken(r)
	if err != nil {
		writeUnauthorized(w)
		return
	}
	claims, err := s.Claims(r.Context(), token)
	if err != nil {
		writeUnauthorized(w)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(claims)
}

func (s *UserInfoService) Claims(ctx context.Context, token string) (map[string]any, error) {
	if s == nil || s.hydra == nil || s.status == nil || s.users == nil || s.apps == nil {
		return nil, ErrUnauthorized
	}
	introspection, err := s.hydra.IntrospectToken(ctx, token)
	if err != nil || !introspection.Active || introspection.Subject == "" || introspection.ClientID == "" {
		return nil, ErrUnauthorized
	}
	app, err := s.apps.GetByClientID(ctx, introspection.ClientID)
	if err != nil || app.Status != domain.StatusApproved {
		return nil, ErrUnauthorized
	}
	status, err := s.status.CurrentStatus(ctx, introspection.Subject)
	if err != nil || !status.CanAuthenticate() {
		return nil, ErrUnauthorized
	}
	user, err := s.users.GetBySubject(ctx, domain.UserID(introspection.Subject))
	if err != nil {
		return nil, ErrUnauthorized
	}
	scopes, err := FilterScopes(strings.Fields(introspection.Scope))
	if err != nil {
		return nil, ErrUnauthorized
	}
	claims := map[string]any{"sub": introspection.Subject}
	if contains(scopes, "profile") {
		claims["preferred_username"] = user.Username
		claims["name"] = user.Name
		claims["picture"] = user.AvatarURL
	}
	if contains(scopes, "email") && strings.TrimSpace(status.Email) != "" {
		claims["email"] = status.Email
	}
	if contains(scopes, "community") {
		claims["trust_level"] = status.TrustLevel
		claims["active"] = status.Active
		claims["silenced"] = status.Silenced
	}
	return claims, nil
}

func bearerToken(r *http.Request) (string, error) {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" ||
		strings.ContainsAny(parts[1], " \t\r\n") {
		return "", ErrUnauthorized
	}
	return parts[1], nil
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer error=\"invalid_token\"")
	writeUserInfoError(w, http.StatusUnauthorized, "invalid_token")
}

func writeUserInfoError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
