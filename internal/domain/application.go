package domain

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

type ApplicationID string
type UserID string
type ClientID string

type ApplicationStatus string

const (
	StatusDraft            ApplicationStatus = "draft"
	StatusPendingReview    ApplicationStatus = "pending_review"
	StatusProvisioning     ApplicationStatus = "provisioning"
	StatusApproved         ApplicationStatus = "approved"
	StatusRejected         ApplicationStatus = "rejected"
	StatusChangesRequested ApplicationStatus = "changes_requested"
	StatusRevoked          ApplicationStatus = "revoked"
)

var ErrInvalidTransition = errors.New("invalid application status transition")

type Application struct {
	ID                    ApplicationID
	OwnerSubject          string
	Name                  string
	Description           string
	LogoURL               string
	CallbackURLs          []string
	VerifiedDomains       []string
	Status                ApplicationStatus
	RequirePKCENonce      bool
	ClientID              ClientID
	EncryptedClientSecret string
	SecretVersion         int
	ReviewNote            string
	ReviewedBy            string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (a *Application) Transition(to ApplicationStatus, now time.Time) error {
	if !validTransition(a.Status, to) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.Status, to)
	}
	a.Status = to
	a.UpdatedAt = now.UTC()
	return nil
}

func validTransition(from, to ApplicationStatus) bool {
	switch from {
	case StatusDraft:
		return to == StatusPendingReview || to == StatusProvisioning
	case StatusPendingReview:
		return to == StatusProvisioning || to == StatusRejected || to == StatusChangesRequested
	case StatusProvisioning:
		return to == StatusApproved || to == StatusRejected
	case StatusApproved:
		return to == StatusRevoked
	case StatusRejected, StatusChangesRequested:
		return to == StatusDraft
	default:
		return false
	}
}

func (a Application) IsOpen() bool {
	switch a.Status {
	case StatusDraft, StatusPendingReview, StatusProvisioning, StatusChangesRequested:
		return true
	default:
		return false
	}
}

func (a Application) Clone() Application {
	copy := a
	copy.CallbackURLs = append([]string(nil), a.CallbackURLs...)
	copy.VerifiedDomains = append([]string(nil), a.VerifiedDomains...)
	return copy
}

// ValidateCallbackURL enforces the first-version confidential web-client
// contract: exact HTTPS URLs with a public hostname and no wildcard.
func ValidateCallbackURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("callback URL must be an exact HTTPS URL without credentials, query, or fragment")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || strings.Contains(host, "*") || host == "localhost" || net.ParseIP(host) != nil {
		return errors.New("callback URL hostname must be a public domain")
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return nil
}

func ValidateApplicationInput(name, ownerSubject string, callbacks []string) error {
	if strings.TrimSpace(ownerSubject) == "" {
		return errors.New("owner subject is required")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return errors.New("application name must contain 1 to 120 characters")
	}
	if len(callbacks) == 0 || len(callbacks) > 10 {
		return errors.New("application must contain 1 to 10 callback URLs")
	}
	for _, callback := range callbacks {
		if err := ValidateCallbackURL(callback); err != nil {
			return fmt.Errorf("callback %q: %w", callback, err)
		}
	}
	return nil
}
