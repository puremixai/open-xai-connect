package apps

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/identity"
	"connect.xai.run/internal/secrets"
	"connect.xai.run/internal/store"
)

var (
	ErrNotEligible           = errors.New("user is not eligible to manage applications")
	ErrOpenApplicationLimit  = errors.New("open application limit reached")
	ErrNotOwner              = errors.New("application owner required")
	ErrInvalidState          = errors.New("application is not editable in its current state")
	ErrInvalidInput          = errors.New("invalid application input")
	ErrSensitiveConfirmation = errors.New("recent sensitive-action confirmation required")
)

type DraftInput struct {
	Name            string
	Description     string
	LogoURL         string
	CallbackURLs    []string
	VerifiedDomains []string
}

type Dependencies struct {
	Status          identity.StatusLookup
	Apps            store.ApplicationRepository
	Audit           store.AuditRepository
	Box             *secrets.Box
	Hydra           hydra.Client
	MaxOpen         int
	SensitiveWindow time.Duration
}

type Service struct {
	status          identity.StatusLookup
	apps            store.ApplicationRepository
	audit           store.AuditRepository
	box             *secrets.Box
	hydra           hydra.Client
	maxOpen         int
	sensitiveWindow time.Duration
	now             func() time.Time
}

func NewService(deps Dependencies) *Service {
	maxOpen := deps.MaxOpen
	if maxOpen <= 0 {
		maxOpen = 3
	}
	window := deps.SensitiveWindow
	if window <= 0 {
		window = 5 * time.Minute
	}
	return &Service{
		status: deps.Status, apps: deps.Apps, audit: deps.Audit,
		box: deps.Box, hydra: deps.Hydra, maxOpen: maxOpen,
		sensitiveWindow: window, now: time.Now,
	}
}

func (s *Service) CreateDraft(ctx context.Context, subject string, input DraftInput) (domain.Application, error) {
	if err := s.ensureEligible(ctx, subject); err != nil {
		return domain.Application{}, err
	}
	if s.apps == nil {
		return domain.Application{}, errors.New("application repository is not initialized")
	}
	count, err := s.apps.CountOpenByOwner(ctx, subject)
	if err != nil {
		return domain.Application{}, err
	}
	if count >= s.maxOpen {
		return domain.Application{}, ErrOpenApplicationLimit
	}
	if err := validateInput(subject, input); err != nil {
		return domain.Application{}, err
	}
	now := s.now().UTC()
	id, err := newApplicationID()
	if err != nil {
		return domain.Application{}, err
	}
	app := domain.Application{
		ID: id, OwnerSubject: subject, Name: strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description), LogoURL: strings.TrimSpace(input.LogoURL),
		CallbackURLs:    append([]string(nil), input.CallbackURLs...),
		VerifiedDomains: normalizeDomains(input.VerifiedDomains),
		Status:          domain.StatusDraft, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.apps.Create(ctx, app); err != nil {
		return domain.Application{}, err
	}
	if err := s.auditEvent(ctx, subject, app.ID, "application.created"); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Service) UpdateDraft(ctx context.Context, subject string, id domain.ApplicationID, input DraftInput) (domain.Application, error) {
	if err := s.ensureEligible(ctx, subject); err != nil {
		return domain.Application{}, err
	}
	app, err := s.getOwned(ctx, subject, id)
	if err != nil {
		return domain.Application{}, err
	}
	if app.Status != domain.StatusDraft && app.Status != domain.StatusChangesRequested && app.Status != domain.StatusRejected {
		return domain.Application{}, ErrInvalidState
	}
	if err := validateInput(subject, input); err != nil {
		return domain.Application{}, err
	}
	app.Name = strings.TrimSpace(input.Name)
	app.Description = strings.TrimSpace(input.Description)
	app.LogoURL = strings.TrimSpace(input.LogoURL)
	app.CallbackURLs = append([]string(nil), input.CallbackURLs...)
	app.VerifiedDomains = normalizeDomains(input.VerifiedDomains)
	app.ReviewNote = ""
	app.ReviewedBy = ""
	app.Status = domain.StatusDraft
	app.UpdatedAt = s.now().UTC()
	if err := s.apps.Save(ctx, app); err != nil {
		return domain.Application{}, err
	}
	if err := s.auditEvent(ctx, subject, id, "application.updated"); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Service) Submit(ctx context.Context, subject string, id domain.ApplicationID) (domain.Application, error) {
	if err := s.ensureEligible(ctx, subject); err != nil {
		return domain.Application{}, err
	}
	app, err := s.getOwned(ctx, subject, id)
	if err != nil {
		return domain.Application{}, err
	}
	if app.Status == domain.StatusChangesRequested || app.Status == domain.StatusRejected {
		app, err = s.apps.Transition(ctx, id, app.Status, domain.StatusDraft, s.now().UTC())
		if err != nil {
			return domain.Application{}, err
		}
	}
	if app.Status != domain.StatusDraft {
		return domain.Application{}, ErrInvalidState
	}
	app, err = s.apps.Transition(ctx, id, domain.StatusDraft, domain.StatusPendingReview, s.now().UTC())
	if err != nil {
		return domain.Application{}, err
	}
	if err := s.auditEvent(ctx, subject, id, "application.submitted"); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Service) ListMine(ctx context.Context, subject string) ([]domain.Application, error) {
	if s.apps == nil {
		return nil, errors.New("application repository is not initialized")
	}
	return s.apps.ListByOwner(ctx, subject)
}

func (s *Service) GetMine(ctx context.Context, subject string, id domain.ApplicationID) (domain.Application, error) {
	return s.getOwned(ctx, subject, id)
}

func (s *Service) ViewSecret(ctx context.Context, subject string, id domain.ApplicationID, confirmedAt time.Time) (string, error) {
	app, err := s.getOwned(ctx, subject, id)
	if err != nil {
		return "", err
	}
	if app.Status != domain.StatusApproved || app.ClientID == "" || app.EncryptedClientSecret == "" {
		return "", ErrInvalidState
	}
	if !s.confirmed(confirmedAt) {
		return "", ErrSensitiveConfirmation
	}
	if s.box == nil {
		return "", errors.New("secret encryption is not initialized")
	}
	secret, err := s.box.Decrypt(app.EncryptedClientSecret, secretAssociatedData(app))
	if err != nil {
		return "", err
	}
	if err := s.auditEvent(ctx, subject, id, "application.secret_viewed"); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) RotateSecret(ctx context.Context, subject string, id domain.ApplicationID, confirmedAt time.Time) (domain.Application, error) {
	app, err := s.getOwned(ctx, subject, id)
	if err != nil {
		return domain.Application{}, err
	}
	if app.Status != domain.StatusApproved || app.ClientID == "" {
		return domain.Application{}, ErrInvalidState
	}
	if !s.confirmed(confirmedAt) {
		return domain.Application{}, ErrSensitiveConfirmation
	}
	if s.box == nil || s.hydra == nil {
		return domain.Application{}, errors.New("secret rotation dependencies are not initialized")
	}
	plaintext, err := randomClientSecret()
	if err != nil {
		return domain.Application{}, err
	}
	credentials, err := s.hydra.UpdateClient(ctx, string(app.ClientID), hydra.ClientRegistration{
		ClientID: string(app.ClientID), ClientName: app.Name, LogoURI: app.LogoURL,
		RedirectURIs:  append([]string(nil), app.CallbackURLs...),
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, Scope: "openid profile community offline_access",
		TokenEndpointAuthMethod: "client_secret_basic", Owner: app.OwnerSubject,
		ClientSecret: plaintext,
	})
	if err != nil {
		return domain.Application{}, err
	}
	if credentials.ID != string(app.ClientID) || credentials.Secret == "" {
		return domain.Application{}, errors.New("Hydra returned invalid rotated credentials")
	}
	encrypted, err := s.box.Encrypt(credentials.Secret, secretAssociatedData(app))
	if err != nil {
		return domain.Application{}, err
	}
	app.EncryptedClientSecret = encrypted
	app.SecretVersion++
	app.UpdatedAt = s.now().UTC()
	if err := s.apps.Save(ctx, app); err != nil {
		return domain.Application{}, err
	}
	if err := s.auditEvent(ctx, subject, id, "application.secret_rotated"); err != nil {
		return domain.Application{}, err
	}
	return app, nil
}

func (s *Service) Revoke(ctx context.Context, subject string, id domain.ApplicationID) error {
	app, err := s.getOwned(ctx, subject, id)
	if err != nil {
		return err
	}
	if app.Status != domain.StatusApproved {
		return ErrInvalidState
	}
	if s.hydra == nil {
		return errors.New("Hydra client is not initialized")
	}
	if err := s.hydra.DeleteClient(ctx, string(app.ClientID)); err != nil {
		return err
	}
	if _, err := s.apps.Transition(ctx, id, domain.StatusApproved, domain.StatusRevoked, s.now().UTC()); err != nil {
		return err
	}
	return s.auditEvent(ctx, subject, id, "application.revoked")
}

func (s *Service) ensureEligible(ctx context.Context, subject string) error {
	if s == nil || s.status == nil || strings.TrimSpace(subject) == "" {
		return ErrNotEligible
	}
	status, err := s.status.CurrentStatus(ctx, subject)
	if err != nil {
		return err
	}
	if !status.CanManageApplications() {
		return ErrNotEligible
	}
	return nil
}

func (s *Service) getOwned(ctx context.Context, subject string, id domain.ApplicationID) (domain.Application, error) {
	if s == nil || s.apps == nil {
		return domain.Application{}, errors.New("application repository is not initialized")
	}
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return domain.Application{}, err
	}
	if app.OwnerSubject != subject {
		return domain.Application{}, ErrNotOwner
	}
	return app, nil
}

func (s *Service) confirmed(confirmedAt time.Time) bool {
	if confirmedAt.IsZero() {
		return false
	}
	now := s.now().UTC()
	age := now.Sub(confirmedAt.UTC())
	return age >= 0 && age <= s.sensitiveWindow
}

func (s *Service) auditEvent(ctx context.Context, actor string, id domain.ApplicationID, action string) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Append(ctx, domain.AuditEvent{
		ID: newAuditID(), ActorSubject: actor, Action: action,
		ApplicationID: id, Metadata: map[string]string{"result": "success"},
		CreatedAt: s.now().UTC(),
	})
}

func validateInput(subject string, input DraftInput) error {
	if err := domain.ValidateApplicationInput(input.Name, subject, input.CallbackURLs); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if len(input.VerifiedDomains) == 0 {
		return fmt.Errorf("%w: at least one verified domain is required", ErrInvalidInput)
	}
	domains := normalizeDomains(input.VerifiedDomains)
	for _, callback := range input.CallbackURLs {
		parsed, _ := url.Parse(callback)
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		matched := false
		for _, domainName := range domains {
			if host == domainName || strings.HasSuffix(host, "."+domainName) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: callback host %s is not covered by a verified domain", ErrInvalidInput, host)
		}
	}
	if input.LogoURL != "" && (!strings.HasPrefix(input.LogoURL, "/assets/") || strings.Contains(input.LogoURL, "..")) {
		return fmt.Errorf("%w: logo must be Connect-hosted", ErrInvalidInput)
	}
	return nil
}

func normalizeDomains(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
		if value == "" {
			continue
		}
		parsed, err := url.Parse("https://" + value)
		if err != nil || parsed.Hostname() != value || strings.Contains(value, "*") || net.ParseIP(value) != nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func newApplicationID() (domain.ApplicationID, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return domain.ApplicationID("app_" + base64.RawURLEncoding.EncodeToString(raw)), nil
}

func randomClientSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newAuditID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "audit_fallback"
	}
	return "audit_" + base64.RawURLEncoding.EncodeToString(raw)
}

func secretAssociatedData(app domain.Application) string {
	return string(app.ID) + ":" + string(app.ClientID)
}
