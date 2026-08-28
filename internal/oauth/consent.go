package oauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/store"
)

var (
	ErrConsentDenied  = errors.New("consent denied")
	ErrAppUnavailable = errors.New("application is not available")
)

type ConsentService struct {
	hydra       hydra.Client
	status      identity.StatusLookup
	apps        store.ApplicationRepository
	consents    store.ConsentRepository
	rememberFor time.Duration
	now         func() time.Time
}

func NewConsentService(hydraClient hydra.Client, status identity.StatusLookup, apps store.ApplicationRepository, consents store.ConsentRepository, rememberFor time.Duration) *ConsentService {
	if rememberFor <= 0 {
		rememberFor = 180 * 24 * time.Hour
	}
	return &ConsentService{
		hydra: hydraClient, status: status, apps: apps, consents: consents,
		rememberFor: rememberFor, now: time.Now,
	}
}

func (s *ConsentService) Request(ctx context.Context, challenge string) (hydra.ConsentRequest, error) {
	if s == nil || s.hydra == nil || s.apps == nil {
		return hydra.ConsentRequest{}, errors.New("consent service is not initialized")
	}
	request, err := s.hydra.GetConsentRequest(ctx, challenge)
	if err != nil {
		return hydra.ConsentRequest{}, err
	}
	if _, err := FilterScopes(request.RequestedScope); err != nil {
		return hydra.ConsentRequest{}, err
	}
	app, err := s.apps.GetByClientID(ctx, request.Client.ID)
	if err != nil {
		return hydra.ConsentRequest{}, err
	}
	if app.Status != domain.StatusApproved || app.ClientID == "" {
		return hydra.ConsentRequest{}, ErrAppUnavailable
	}
	return request, nil
}

func (s *ConsentService) Accept(ctx context.Context, challenge, subject string, granted []string, remember bool) (string, error) {
	if s == nil || s.status == nil || s.consents == nil {
		return "", errors.New("consent service is not initialized")
	}
	request, err := s.Request(ctx, challenge)
	if err != nil {
		return "", err
	}
	if request.Subject != subject || subject == "" {
		return "", ErrConsentDenied
	}
	status, err := s.status.CurrentStatus(ctx, subject)
	if err != nil {
		return "", err
	}
	if !status.CanAuthenticate() {
		return "", ErrConsentDenied
	}
	requested, err := FilterScopes(request.RequestedScope)
	if err != nil {
		return "", err
	}
	selected, err := FilterScopes(granted)
	if err != nil {
		return "", err
	}
	for _, scope := range selected {
		if !contains(requested, scope) {
			return "", fmt.Errorf("%w: scope %s was not requested", ErrConsentDenied, scope)
		}
	}
	app, err := s.apps.GetByClientID(ctx, request.Client.ID)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	if err := s.consents.Put(ctx, domain.Consent{
		UserSubject: domain.UserID(subject), ApplicationID: app.ID,
		Scopes: selected, Remember: remember, GrantedAt: now, LastUsedAt: now,
	}); err != nil {
		return "", err
	}
	var idTokenClaims map[string]any
	if contains(selected, "email") && strings.TrimSpace(status.Email) != "" {
		idTokenClaims = map[string]any{"email": status.Email}
	}
	redirect, err := s.hydra.AcceptConsent(ctx, challenge, hydra.ConsentAcceptance{
		GrantScope: selected, Remember: remember, IDTokenClaims: idTokenClaims,
		RememberFor: int64(s.rememberFor / time.Second),
	})
	if err != nil {
		if deleteErr := s.consents.Delete(ctx, domain.UserID(subject), app.ID); deleteErr != nil {
			return "", fmt.Errorf("Hydra consent failed and rollback failed: %w", deleteErr)
		}
		return "", err
	}
	return redirect, nil
}

func (s *ConsentService) Reject(ctx context.Context, challenge, reason string) (string, error) {
	if s == nil || s.hydra == nil {
		return "", errors.New("consent service is not initialized")
	}
	if reason == "" {
		reason = "user_denied"
	}
	return s.hydra.RejectConsent(ctx, challenge, hydra.RejectRequest{
		Error: "access_denied", ErrorDescription: reason, StatusCode: 403,
	})
}
