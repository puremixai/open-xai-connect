package apps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/hydra"
	"connect.xai.run/internal/secrets"
	"connect.xai.run/internal/store"
)

type ProvisionerDependencies struct {
	Apps       store.ApplicationRepository
	Outbox     store.OutboxRepository
	Audit      store.AuditRepository
	Hydra      hydra.Client
	Box        *secrets.Box
	Now        func() time.Time
	RetryDelay time.Duration
}

type Provisioner struct {
	apps       store.ApplicationRepository
	outbox     store.OutboxRepository
	audit      store.AuditRepository
	hydra      hydra.Client
	box        *secrets.Box
	now        func() time.Time
	retryDelay time.Duration
}

func NewProvisioner(deps ProvisionerDependencies) *Provisioner {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	retryDelay := deps.RetryDelay
	if retryDelay <= 0 {
		retryDelay = time.Minute
	}
	return &Provisioner{
		apps: deps.Apps, outbox: deps.Outbox, audit: deps.Audit,
		hydra: deps.Hydra, box: deps.Box, now: now, retryDelay: retryDelay,
	}
}

func (p *Provisioner) RunOnce(ctx context.Context) (bool, error) {
	if p == nil || p.apps == nil || p.outbox == nil || p.hydra == nil || p.box == nil {
		return false, errors.New("provisioner dependencies are not initialized")
	}
	now := p.now().UTC()
	event, err := p.outbox.ClaimNext(ctx, now)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if event.Kind != "application.provision" {
		_ = p.outbox.Complete(ctx, event.ID, now)
		return true, fmt.Errorf("unsupported outbox event kind %q", event.Kind)
	}
	app, err := p.apps.Get(ctx, event.ApplicationID)
	if err != nil {
		_ = p.retry(ctx, event.ID, now)
		return false, err
	}
	if app.Status != domain.StatusProvisioning {
		_ = p.outbox.Complete(ctx, event.ID, now)
		return true, nil
	}
	credentials, err := p.hydra.CreateClient(ctx, hydra.ClientRegistration{
		ClientName: app.Name, LogoURI: app.LogoURL, RedirectURIs: append([]string(nil), app.CallbackURLs...),
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"}, Scope: "openid profile community offline_access",
		TokenEndpointAuthMethod: "client_secret_basic", Owner: app.OwnerSubject,
		AccessTokenStrategy: "opaque", IDTokenLifespan: "1h",
		AccessTokenLifespan: "24h", RefreshTokenLifespan: "4320h",
	})
	if err != nil {
		_ = p.retry(ctx, event.ID, now)
		return false, err
	}
	if credentials.ID == "" || credentials.Secret == "" {
		_ = p.retry(ctx, event.ID, now)
		return false, errors.New("Hydra returned incomplete provisioning credentials")
	}
	app.ClientID = domain.ClientID(credentials.ID)
	app.EncryptedClientSecret, err = p.box.Encrypt(credentials.Secret, secretAssociatedData(app))
	if err != nil {
		_ = p.retry(ctx, event.ID, now)
		return false, err
	}
	app.SecretVersion = 1
	app.Status = domain.StatusApproved
	app.UpdatedAt = now
	if err := p.apps.Save(ctx, app); err != nil {
		_ = p.retry(ctx, event.ID, now)
		return false, err
	}
	if err := p.outbox.Complete(ctx, event.ID, now); err != nil {
		return false, err
	}
	if p.audit != nil {
		if err := p.audit.Append(ctx, domain.AuditEvent{
			ID: newAuditID(), ActorSubject: "system", Action: "application.provisioned",
			ApplicationID: app.ID, Metadata: map[string]string{"result": "success"},
			CreatedAt: now,
		}); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (p *Provisioner) retry(ctx context.Context, eventID string, now time.Time) error {
	if p.outbox == nil {
		return nil
	}
	return p.outbox.Retry(ctx, eventID, now.Add(p.retryDelay))
}

func ValidateProvisioningRegistration(registration hydra.ClientRegistration) error {
	if strings.TrimSpace(registration.ClientName) == "" ||
		len(registration.RedirectURIs) == 0 ||
		registration.TokenEndpointAuthMethod != "client_secret_basic" ||
		len(registration.ResponseTypes) != 1 || registration.ResponseTypes[0] != "code" {
		return errors.New("registration does not satisfy confidential code-client contract")
	}
	return nil
}
