package oauth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/store"
)

var ErrLoginDenied = errors.New("login denied")

type LoginService struct {
	hydra       hydra.Client
	status      identity.StatusLookup
	apps        store.ApplicationRepository
	rememberFor time.Duration
}

func NewLoginService(hydraClient hydra.Client, status identity.StatusLookup, apps store.ApplicationRepository, rememberFor time.Duration) *LoginService {
	if rememberFor <= 0 {
		rememberFor = 30 * 24 * time.Hour
	}
	return &LoginService{hydra: hydraClient, status: status, apps: apps, rememberFor: rememberFor}
}

func (s *LoginService) Begin(ctx context.Context, challenge string) (hydra.LoginRequest, error) {
	if s == nil || s.hydra == nil {
		return hydra.LoginRequest{}, errors.New("login service is not initialized")
	}
	request, err := s.hydra.GetLoginRequest(ctx, challenge)
	if err != nil {
		return hydra.LoginRequest{}, err
	}
	requirePKCENonce := true
	if s.apps != nil {
		app, err := s.apps.GetByClientID(ctx, request.Client.ID)
		if err != nil {
			return hydra.LoginRequest{}, fmt.Errorf("load OAuth application settings: %w", err)
		}
		requirePKCENonce = app.RequirePKCENonce
	}
	if err := ValidateLoginRequestWithPKCENonce(request, requirePKCENonce); err != nil {
		return hydra.LoginRequest{}, fmt.Errorf("%w: %v", ErrLoginDenied, err)
	}
	return request, nil
}

func (s *LoginService) Complete(ctx context.Context, challenge, subject string) (string, error) {
	if s == nil || s.status == nil {
		return "", errors.New("login service is not initialized")
	}
	request, err := s.Begin(ctx, challenge)
	if err != nil {
		return "", err
	}
	if subject == "" || (request.Subject != "" && request.Subject != subject) {
		return "", ErrLoginDenied
	}
	status, err := s.status.CurrentStatus(ctx, subject)
	if err != nil {
		return "", err
	}
	if !status.CanAuthenticate() {
		return "", ErrLoginDenied
	}
	return s.hydra.AcceptLogin(ctx, challenge, hydra.LoginAcceptance{
		Subject: subject, Remember: true,
		RememberFor: int64(s.rememberFor / time.Second),
		SessionID:   request.SessionID,
	})
}

func (s *LoginService) Reject(ctx context.Context, challenge, reason string) (string, error) {
	if s == nil || s.hydra == nil {
		return "", errors.New("login service is not initialized")
	}
	if reason == "" {
		reason = "login_required"
	}
	return s.hydra.RejectLogin(ctx, challenge, hydra.RejectRequest{
		Error: "login_required", ErrorDescription: reason, StatusCode: 401,
	})
}
