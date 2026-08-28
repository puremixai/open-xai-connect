package hydra

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	ory "github.com/ory/hydra-client-go/v26"
)

type OryClient struct {
	API *ory.APIClient
}

func NewOryClient(baseURL string, httpClient *http.Client) (*OryClient, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("Hydra admin URL must be absolute")
	}
	cfg := ory.NewConfiguration()
	cfg.Servers[0].URL = strings.TrimRight(baseURL, "/")
	cfg.HTTPClient = httpClient
	cfg.UserAgent = "xai-connect/1.0"
	return &OryClient{API: ory.NewAPIClient(cfg)}, nil
}

func (c *OryClient) CreateClient(ctx context.Context, registration ClientRegistration) (ClientCredentials, error) {
	if c == nil || c.API == nil {
		return ClientCredentials{}, errors.New("Hydra client is not initialized")
	}
	model := *ory.NewOAuth2Client()
	model.ClientId = optionalString(registration.ClientID)
	model.ClientSecret = optionalString(registration.ClientSecret)
	model.ClientName = optionalString(registration.ClientName)
	model.ClientUri = optionalString(registration.ClientURI)
	model.PolicyUri = optionalString(registration.PolicyURI)
	model.LogoUri = optionalString(registration.LogoURI)
	model.RedirectUris = append([]string(nil), registration.RedirectURIs...)
	model.GrantTypes = append([]string(nil), registration.GrantTypes...)
	model.ResponseTypes = append([]string(nil), registration.ResponseTypes...)
	model.Scope = optionalString(registration.Scope)
	model.Owner = optionalString(registration.Owner)
	if registration.TokenEndpointAuthMethod != "" {
		model.TokenEndpointAuthMethod = optionalString(registration.TokenEndpointAuthMethod)
	}
	model.AccessTokenStrategy = optionalString(registration.AccessTokenStrategy)
	model.AuthorizationCodeGrantIdTokenLifespan = optionalString(registration.IDTokenLifespan)
	model.AuthorizationCodeGrantAccessTokenLifespan = optionalString(registration.AccessTokenLifespan)
	model.AuthorizationCodeGrantRefreshTokenLifespan = optionalString(registration.RefreshTokenLifespan)
	created, _, err := c.API.OAuth2API.CreateOAuth2Client(ctx).OAuth2Client(model).Execute()
	if err != nil {
		return ClientCredentials{}, fmt.Errorf("create Hydra client: %w", err)
	}
	if created == nil || created.GetClientId() == "" || created.GetClientSecret() == "" {
		return ClientCredentials{}, errors.New("Hydra returned incomplete client credentials")
	}
	return ClientCredentials{ID: created.GetClientId(), Secret: created.GetClientSecret()}, nil
}

func (c *OryClient) UpdateClient(ctx context.Context, clientID string, registration ClientRegistration) (ClientCredentials, error) {
	if c == nil || c.API == nil {
		return ClientCredentials{}, errors.New("Hydra client is not initialized")
	}
	if clientID == "" {
		return ClientCredentials{}, errors.New("Hydra client ID is required")
	}
	registration.ClientID = clientID
	model := registrationModel(registration)
	updated, _, err := c.API.OAuth2API.SetOAuth2Client(ctx, clientID).OAuth2Client(model).Execute()
	if err != nil {
		return ClientCredentials{}, fmt.Errorf("update Hydra client: %w", err)
	}
	if updated == nil || updated.GetClientId() == "" || updated.GetClientSecret() == "" {
		return ClientCredentials{}, errors.New("Hydra returned incomplete rotated credentials")
	}
	return ClientCredentials{ID: updated.GetClientId(), Secret: updated.GetClientSecret()}, nil
}

func (c *OryClient) DeleteClient(ctx context.Context, clientID string) error {
	if clientID == "" {
		return errors.New("Hydra client ID is required")
	}
	if _, err := c.API.OAuth2API.DeleteOAuth2Client(ctx, clientID).Execute(); err != nil {
		return fmt.Errorf("delete Hydra client: %w", err)
	}
	return nil
}

func (c *OryClient) GetLoginRequest(ctx context.Context, challenge string) (LoginRequest, error) {
	request, _, err := c.API.OAuth2API.GetOAuth2LoginRequest(ctx).LoginChallenge(challenge).Execute()
	if err != nil {
		return LoginRequest{}, fmt.Errorf("get Hydra login request: %w", err)
	}
	if request == nil {
		return LoginRequest{}, errors.New("Hydra returned an empty login request")
	}
	return LoginRequest{
		Challenge: request.Challenge, Client: mapClient(request.Client),
		RequestedScope: append([]string(nil), request.RequestedScope...),
		Subject:        request.Subject, Skip: request.Skip, SessionID: request.GetSessionId(),
		RequestURL: request.RequestUrl,
	}, nil
}

func (c *OryClient) AcceptLogin(ctx context.Context, challenge string, acceptance LoginAcceptance) (string, error) {
	payload := ory.NewAcceptOAuth2LoginRequest(acceptance.Subject)
	payload.Remember = optionalBool(acceptance.Remember)
	if acceptance.RememberFor > 0 {
		payload.RememberFor = optionalInt64(acceptance.RememberFor)
	}
	payload.IdentityProviderSessionId = optionalString(acceptance.SessionID)
	payload.Acr = optionalString(acceptance.Acr)
	payload.Amr = append([]string(nil), acceptance.Amr...)
	if acceptance.Context != nil {
		payload.Context = acceptance.Context
	}
	redirect, _, err := c.API.OAuth2API.AcceptOAuth2LoginRequest(ctx).
		LoginChallenge(challenge).AcceptOAuth2LoginRequest(*payload).Execute()
	if err != nil {
		return "", fmt.Errorf("accept Hydra login: %w", err)
	}
	return redirectURL(redirect)
}

func (c *OryClient) RejectLogin(ctx context.Context, challenge string, rejection RejectRequest) (string, error) {
	redirect, _, err := c.API.OAuth2API.RejectOAuth2LoginRequest(ctx).
		LoginChallenge(challenge).RejectOAuth2Request(rejectModel(rejection)).Execute()
	if err != nil {
		return "", fmt.Errorf("reject Hydra login: %w", err)
	}
	return redirectURL(redirect)
}

func (c *OryClient) GetConsentRequest(ctx context.Context, challenge string) (ConsentRequest, error) {
	request, _, err := c.API.OAuth2API.GetOAuth2ConsentRequest(ctx).ConsentChallenge(challenge).Execute()
	if err != nil {
		return ConsentRequest{}, fmt.Errorf("get Hydra consent request: %w", err)
	}
	if request == nil || request.Client == nil {
		return ConsentRequest{}, errors.New("Hydra returned an empty consent request")
	}
	return ConsentRequest{
		Challenge: request.Challenge, Client: mapClient(*request.Client),
		RequestedScope: append([]string(nil), request.RequestedScope...),
		Subject:        request.GetSubject(), Skip: request.GetSkip(),
		LoginChallenge: request.GetLoginChallenge(), SessionID: request.GetLoginSessionId(),
		RequestURL: request.GetRequestUrl(),
	}, nil
}

func (c *OryClient) AcceptConsent(ctx context.Context, challenge string, acceptance ConsentAcceptance) (string, error) {
	payload := ory.NewAcceptOAuth2ConsentRequest()
	payload.GrantScope = append([]string(nil), acceptance.GrantScope...)
	payload.Remember = optionalBool(acceptance.Remember)
	if acceptance.RememberFor > 0 {
		payload.RememberFor = optionalInt64(acceptance.RememberFor)
	}
	if acceptance.AccessTokenClaims != nil || acceptance.IDTokenClaims != nil {
		session := ory.NewAcceptOAuth2ConsentRequestSession()
		session.AccessToken = acceptance.AccessTokenClaims
		session.IdToken = acceptance.IDTokenClaims
		payload.Session = session
	}
	redirect, _, err := c.API.OAuth2API.AcceptOAuth2ConsentRequest(ctx).
		ConsentChallenge(challenge).AcceptOAuth2ConsentRequest(*payload).Execute()
	if err != nil {
		return "", fmt.Errorf("accept Hydra consent: %w", err)
	}
	return redirectURL(redirect)
}

func (c *OryClient) RejectConsent(ctx context.Context, challenge string, rejection RejectRequest) (string, error) {
	redirect, _, err := c.API.OAuth2API.RejectOAuth2ConsentRequest(ctx).
		ConsentChallenge(challenge).RejectOAuth2Request(rejectModel(rejection)).Execute()
	if err != nil {
		return "", fmt.Errorf("reject Hydra consent: %w", err)
	}
	return redirectURL(redirect)
}

func (c *OryClient) RevokeSession(ctx context.Context, subject, sid string) error {
	request := c.API.OAuth2API.RevokeOAuth2LoginSessions(ctx)
	if sid != "" {
		request = request.Sid(sid)
	} else if subject != "" {
		request = request.Subject(subject)
	} else {
		return errors.New("subject or session ID is required")
	}
	if _, err := request.Execute(); err != nil {
		return fmt.Errorf("revoke Hydra session: %w", err)
	}
	return nil
}

func (c *OryClient) IntrospectToken(ctx context.Context, token string) (TokenIntrospection, error) {
	if token == "" {
		return TokenIntrospection{}, errors.New("token is required")
	}
	result, _, err := c.API.OAuth2API.IntrospectOAuth2Token(ctx).Token(token).Execute()
	if err != nil {
		return TokenIntrospection{}, fmt.Errorf("introspect Hydra token: %w", err)
	}
	if result == nil {
		return TokenIntrospection{}, errors.New("Hydra returned an empty token response")
	}
	return TokenIntrospection{
		Active: result.Active, Subject: result.GetSub(), ClientID: result.GetClientId(),
		Scope: result.GetScope(), Ext: result.Ext,
	}, nil
}

func registrationModel(registration ClientRegistration) ory.OAuth2Client {
	model := *ory.NewOAuth2Client()
	model.ClientId = optionalString(registration.ClientID)
	model.ClientName = optionalString(registration.ClientName)
	model.ClientUri = optionalString(registration.ClientURI)
	model.PolicyUri = optionalString(registration.PolicyURI)
	model.LogoUri = optionalString(registration.LogoURI)
	model.RedirectUris = append([]string(nil), registration.RedirectURIs...)
	model.GrantTypes = append([]string(nil), registration.GrantTypes...)
	model.ResponseTypes = append([]string(nil), registration.ResponseTypes...)
	model.Scope = optionalString(registration.Scope)
	model.Owner = optionalString(registration.Owner)
	if registration.TokenEndpointAuthMethod != "" {
		model.TokenEndpointAuthMethod = optionalString(registration.TokenEndpointAuthMethod)
	}
	model.AccessTokenStrategy = optionalString(registration.AccessTokenStrategy)
	model.AuthorizationCodeGrantIdTokenLifespan = optionalString(registration.IDTokenLifespan)
	model.AuthorizationCodeGrantAccessTokenLifespan = optionalString(registration.AccessTokenLifespan)
	model.AuthorizationCodeGrantRefreshTokenLifespan = optionalString(registration.RefreshTokenLifespan)
	model.ClientSecret = optionalString(registration.ClientSecret)
	return model
}

func mapClient(client ory.OAuth2Client) ClientInfo {
	return ClientInfo{
		ID: client.GetClientId(), Name: client.GetClientName(), URI: client.GetClientUri(),
		PolicyURI: client.GetPolicyUri(), LogoURI: client.GetLogoUri(),
		RedirectURIs: append([]string(nil), client.GetRedirectUris()...),
	}
}

func redirectURL(value *ory.OAuth2RedirectTo) (string, error) {
	if value == nil || value.GetRedirectTo() == "" {
		return "", errors.New("Hydra returned an empty redirect")
	}
	return value.GetRedirectTo(), nil
}

func rejectModel(rejection RejectRequest) ory.RejectOAuth2Request {
	model := ory.NewRejectOAuth2Request()
	model.Error = optionalString(rejection.Error)
	model.ErrorDescription = optionalString(rejection.ErrorDescription)
	model.ErrorHint = optionalString(rejection.ErrorHint)
	if rejection.StatusCode > 0 {
		model.StatusCode = optionalInt64(rejection.StatusCode)
	}
	return *model
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalBool(value bool) *bool {
	return &value
}

func optionalInt64(value int64) *int64 {
	return &value
}

var _ Client = (*OryClient)(nil)
