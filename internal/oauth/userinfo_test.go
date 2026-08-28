package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/store/memory"
)

func TestUserInfoReturnsMinimumCurrentClaims(t *testing.T) {
	hydraFake := hydra.NewFake()
	hydraFake.Tokens["access-1"] = hydra.TokenIntrospection{
		Active: true, Subject: "sub_1", ClientID: "client_1", Scope: "openid profile community",
	}
	users := memory.NewUserRepository()
	_ = users.Upsert(context.Background(), domain.User{
		Subject: domain.UserID("sub_1"), DiscourseID: 42, Username: "alice", Name: "Alice",
		AvatarURL: "https://forum.example/avatar.png", TrustLevel: 1, Active: true,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	})
	apps := memory.NewApplicationRepository()
	_ = apps.Create(context.Background(), domain.Application{
		ID: domain.ApplicationID("app_1"), OwnerSubject: "owner", Name: "Example",
		Status: domain.StatusApproved, ClientID: domain.ClientID("client_1"),
	})
	service := NewUserInfoService(hydraFake, fakeStatusLookup{status: identity.StatusSnapshot{
		Subject: "sub_1", TrustLevel: 2, Active: true, Silenced: true,
	}}, users, apps)
	request := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	request.Header.Set("Authorization", "Bearer access-1")
	response := httptest.NewRecorder()

	service.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	var claims map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &claims); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	for key, want := range map[string]any{
		"sub": "sub_1", "preferred_username": "alice", "name": "Alice",
		"picture": "https://forum.example/avatar.png", "trust_level": float64(2),
		"active": true, "silenced": true,
	} {
		if claims[key] != want {
			t.Fatalf("claim %s = %#v, want %#v", key, claims[key], want)
		}
	}
	for _, forbidden := range []string{"email", "groups", "discourse_id", "admin", "api_key"} {
		if _, ok := claims[forbidden]; ok {
			t.Fatalf("forbidden claim %q present", forbidden)
		}
	}
}

func TestUserInfoRejectsInactiveTokenAndUnsupportedMethod(t *testing.T) {
	hydraFake := hydra.NewFake()
	hydraFake.Tokens["inactive"] = hydra.TokenIntrospection{Active: false}
	service := NewUserInfoService(hydraFake, fakeStatusLookup{}, memory.NewUserRepository(), memory.NewApplicationRepository())
	request := httptest.NewRequest(http.MethodGet, "/userinfo", nil)
	request.Header.Set("Authorization", "Bearer inactive")
	response := httptest.NewRecorder()
	service.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("inactive status/header = %d/%q", response.Code, response.Header().Get("WWW-Authenticate"))
	}
	post := httptest.NewRequest(http.MethodPost, "/userinfo", nil)
	post.Header.Set("Authorization", "Bearer inactive")
	postResponse := httptest.NewRecorder()
	service.ServeHTTP(postResponse, post)
	if postResponse.Code != http.StatusUnauthorized {
		t.Fatalf("POST inactive status = %d", postResponse.Code)
	}
}
