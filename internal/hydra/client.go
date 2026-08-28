package hydra

import "context"

type Client interface {
	CreateClient(context.Context, ClientRegistration) (ClientCredentials, error)
	UpdateClient(context.Context, string, ClientRegistration) (ClientCredentials, error)
	DeleteClient(context.Context, string) error
	GetLoginRequest(context.Context, string) (LoginRequest, error)
	AcceptLogin(context.Context, string, LoginAcceptance) (string, error)
	RejectLogin(context.Context, string, RejectRequest) (string, error)
	GetConsentRequest(context.Context, string) (ConsentRequest, error)
	AcceptConsent(context.Context, string, ConsentAcceptance) (string, error)
	RejectConsent(context.Context, string, RejectRequest) (string, error)
	RevokeSession(context.Context, string, string) error
	IntrospectToken(context.Context, string) (TokenIntrospection, error)
}

type ClientRegistration struct {
	ClientID                string
	ClientName              string
	ClientSecret            string
	ClientURI               string
	PolicyURI               string
	LogoURI                 string
	RedirectURIs            []string
	GrantTypes              []string
	ResponseTypes           []string
	Scope                   string
	TokenEndpointAuthMethod string
	Owner                   string
	AccessTokenStrategy     string
	IDTokenLifespan         string
	AccessTokenLifespan     string
	RefreshTokenLifespan    string
}

type ClientCredentials struct {
	ID     string
	Secret string
}

type ClientInfo struct {
	ID           string
	Name         string
	URI          string
	PolicyURI    string
	LogoURI      string
	RedirectURIs []string
}

type LoginRequest struct {
	Challenge      string
	Client         ClientInfo
	RequestedScope []string
	Subject        string
	Skip           bool
	SessionID      string
	RequestURL     string
}

type ConsentRequest struct {
	Challenge      string
	Client         ClientInfo
	RequestedScope []string
	Subject        string
	Skip           bool
	LoginChallenge string
	SessionID      string
	RequestURL     string
}

type LoginAcceptance struct {
	Subject     string
	Remember    bool
	RememberFor int64
	SessionID   string
	Acr         string
	Amr         []string
	Context     map[string]any
}

type ConsentAcceptance struct {
	GrantScope        []string
	Remember          bool
	RememberFor       int64
	AccessTokenClaims map[string]any
	IDTokenClaims     map[string]any
}

type RejectRequest struct {
	Error            string
	ErrorDescription string
	ErrorHint        string
	StatusCode       int64
}

type TokenIntrospection struct {
	Active   bool
	Subject  string
	ClientID string
	Scope    string
	Ext      map[string]any
}
