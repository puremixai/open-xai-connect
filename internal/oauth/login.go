package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"connect.xai.run/internal/hydra"
)

var (
	ErrInvalidAuthorization = errors.New("invalid OAuth authorization request")
	allowedScopes           = map[string]struct{}{
		"openid": {}, "profile": {}, "email": {}, "community": {}, "offline_access": {},
	}
)

type AuthorizationRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scope               []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	Nonce               string
}

func ValidateLoginRequest(request hydra.LoginRequest) error {
	if request.Challenge == "" || request.Client.ID == "" || request.RequestURL == "" {
		return ErrInvalidAuthorization
	}
	parsed, err := url.Parse(request.RequestURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "/oauth2/auth" {
		return ErrInvalidAuthorization
	}
	values := parsed.Query()
	if values.Get("client_id") != request.Client.ID {
		return ErrInvalidAuthorization
	}
	redirectURI := values.Get("redirect_uri")
	if redirectURI == "" || !contains(request.Client.RedirectURIs, redirectURI) {
		return ErrInvalidAuthorization
	}
	if values.Get("response_type") != "code" {
		return ErrInvalidAuthorization
	}
	scope, err := FilterScopes(strings.Fields(values.Get("scope")))
	if err != nil || !sameStrings(scope, request.RequestedScope) || !contains(scope, "openid") {
		return ErrInvalidAuthorization
	}
	if values.Get("state") == "" || values.Get("code_challenge") == "" ||
		values.Get("code_challenge_method") != "S256" || values.Get("nonce") == "" {
		return ErrInvalidAuthorization
	}
	return nil
}

func ParseAuthorizationRequest(rawURL string) (AuthorizationRequest, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "/oauth2/auth" {
		return AuthorizationRequest{}, ErrInvalidAuthorization
	}
	values := parsed.Query()
	scope, err := FilterScopes(strings.Fields(values.Get("scope")))
	if err != nil {
		return AuthorizationRequest{}, err
	}
	result := AuthorizationRequest{
		ClientID: values.Get("client_id"), RedirectURI: values.Get("redirect_uri"),
		ResponseType: values.Get("response_type"), Scope: scope, State: values.Get("state"),
		CodeChallenge: values.Get("code_challenge"), CodeChallengeMethod: values.Get("code_challenge_method"),
		Nonce: values.Get("nonce"),
	}
	if result.ClientID == "" || result.RedirectURI == "" || result.ResponseType != "code" ||
		result.State == "" || result.CodeChallenge == "" || result.CodeChallengeMethod != "S256" ||
		result.Nonce == "" {
		return AuthorizationRequest{}, ErrInvalidAuthorization
	}
	return result, nil
}

func FilterScopes(scopes []string) ([]string, error) {
	result := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := allowedScopes[scope]; !ok {
			return nil, fmt.Errorf("%w: scope %s is not allowed", ErrInvalidAuthorization, scope)
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	if len(result) == 0 || !contains(result, "openid") {
		return nil, fmt.Errorf("%w: openid scope is required", ErrInvalidAuthorization)
	}
	return result, nil
}

func sameStrings(left, right []string) bool {
	filtered, err := FilterScopes(right)
	if err != nil || len(left) != len(filtered) {
		return false
	}
	for _, value := range left {
		if !contains(filtered, value) {
			return false
		}
	}
	return true
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
