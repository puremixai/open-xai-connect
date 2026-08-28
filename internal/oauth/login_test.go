package oauth

import (
	"strings"
	"testing"

	"connect.xai.run/internal/hydra"
)

func validLoginRequest() hydra.LoginRequest {
	return hydra.LoginRequest{
		Challenge: "login_challenge",
		Client: hydra.ClientInfo{
			ID: "client_1", Name: "Example",
			RedirectURIs: []string{"https://app.example/callback"},
		},
		RequestedScope: []string{"openid", "profile"},
		RequestURL:     "https://connect.xai.run/oauth2/auth?client_id=client_1&redirect_uri=https%3A%2F%2Fapp.example%2Fcallback&response_type=code&scope=openid+profile&state=state-1&code_challenge=challenge-1&code_challenge_method=S256&nonce=nonce-1",
	}
}

func TestValidateLoginRequestAcceptsCodePKCEAndNonce(t *testing.T) {
	if err := ValidateLoginRequest(validLoginRequest()); err != nil {
		t.Fatalf("ValidateLoginRequest() error = %v", err)
	}
}

func TestValidateLoginRequestRejectsNonCodeOrMissingPKCE(t *testing.T) {
	cases := []struct {
		name string
		edit func(*hydra.LoginRequest)
	}{
		{"implicit response", func(request *hydra.LoginRequest) {
			request.RequestURL = strings.Replace(request.RequestURL, "response_type=code", "response_type=token", 1)
		}},
		{"missing challenge", func(request *hydra.LoginRequest) {
			request.RequestURL = strings.Replace(request.RequestURL, "&code_challenge=challenge-1", "", 1)
		}},
		{"wrong method", func(request *hydra.LoginRequest) {
			request.RequestURL = strings.Replace(request.RequestURL, "code_challenge_method=S256", "code_challenge_method=plain", 1)
		}},
		{"missing nonce", func(request *hydra.LoginRequest) {
			request.RequestURL = strings.Replace(request.RequestURL, "&nonce=nonce-1", "", 1)
		}},
		{"redirect mismatch", func(request *hydra.LoginRequest) {
			request.RequestURL = strings.Replace(request.RequestURL, "app.example%2Fcallback", "evil.example%2Fcallback", 1)
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := validLoginRequest()
			testCase.edit(&request)
			if err := ValidateLoginRequest(request); err == nil {
				t.Fatal("ValidateLoginRequest() error = nil")
			}
		})
	}
}

func TestValidateLoginRequestRejectsUnknownScope(t *testing.T) {
	request := validLoginRequest()
	request.RequestURL = strings.Replace(request.RequestURL, "openid+profile", "openid+email", 1)
	request.RequestedScope = []string{"openid", "email"}
	if err := ValidateLoginRequest(request); err == nil {
		t.Fatal("ValidateLoginRequest() accepted email scope")
	}
}
