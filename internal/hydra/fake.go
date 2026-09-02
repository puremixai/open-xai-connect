package hydra

import (
	"context"
	"errors"
	"sync"
)

type Fake struct {
	mu sync.Mutex

	Registrations    []ClientRegistration
	Updates          []ClientRegistration
	Deleted          []string
	Login            LoginRequest
	Consent          ConsentRequest
	Tokens           map[string]TokenIntrospection
	LastLogin        LoginAcceptance
	LastConsent      ConsentAcceptance
	RedirectURL      string
	Err              error
	AcceptConsentErr error
}

func NewFake() *Fake {
	return &Fake{Tokens: make(map[string]TokenIntrospection), RedirectURL: "https://client.example/continue"}
}

func (f *Fake) CreateClient(_ context.Context, registration ClientRegistration) (ClientCredentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return ClientCredentials{}, f.Err
	}
	f.Registrations = append(f.Registrations, registration)
	id := registration.ClientID
	if id == "" {
		id = "client_" + string(rune('0'+len(f.Registrations)))
	}
	return ClientCredentials{ID: id, Secret: "secret_" + id}, nil
}

func (f *Fake) UpdateClient(_ context.Context, clientID string, registration ClientRegistration) (ClientCredentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return ClientCredentials{}, f.Err
	}
	registration.ClientID = clientID
	f.Updates = append(f.Updates, registration)
	secret := "rotated_" + clientID
	if registration.ClientSecret != "" && registration.AccessTokenStrategy != "" {
		secret = registration.ClientSecret
	}
	return ClientCredentials{ID: clientID, Secret: secret}, nil
}

func (f *Fake) DeleteClient(_ context.Context, clientID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return f.Err
	}
	f.Deleted = append(f.Deleted, clientID)
	return nil
}

func (f *Fake) GetLoginRequest(_ context.Context, _ string) (LoginRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return LoginRequest{}, f.Err
	}
	return f.Login, nil
}

func (f *Fake) AcceptLogin(_ context.Context, _ string, acceptance LoginAcceptance) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return "", f.Err
	}
	f.LastLogin = acceptance
	return f.RedirectURL, nil
}

func (f *Fake) RejectLogin(_ context.Context, _ string, _ RejectRequest) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	return f.RedirectURL, nil
}

func (f *Fake) GetConsentRequest(_ context.Context, _ string) (ConsentRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return ConsentRequest{}, f.Err
	}
	return f.Consent, nil
}

func (f *Fake) AcceptConsent(_ context.Context, _ string, acceptance ConsentAcceptance) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AcceptConsentErr != nil {
		return "", f.AcceptConsentErr
	}
	if f.Err != nil {
		return "", f.Err
	}
	f.LastConsent = acceptance
	return f.RedirectURL, nil
}

func (f *Fake) RejectConsent(_ context.Context, _ string, _ RejectRequest) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	return f.RedirectURL, nil
}

func (f *Fake) RevokeSession(_ context.Context, _, _ string) error {
	return f.Err
}

func (f *Fake) IntrospectToken(_ context.Context, token string) (TokenIntrospection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return TokenIntrospection{}, f.Err
	}
	value, ok := f.Tokens[token]
	if !ok {
		return TokenIntrospection{Active: false}, nil
	}
	return value, nil
}

func (f *Fake) Introspect(ctx context.Context, token string) (TokenIntrospection, error) {
	return f.IntrospectToken(ctx, token)
}

var _ Client = (*Fake)(nil)

var ErrUnavailable = errors.New("Hydra unavailable")
