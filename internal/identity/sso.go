package identity

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
)

var (
	ErrInvalidSSO        = errors.New("invalid DiscourseConnect SSO response")
	ErrSSOStateExpired   = errors.New("DiscourseConnect SSO state expired")
	ErrSSOStateReplay    = errors.New("DiscourseConnect SSO state was already used")
	ErrSSOIdentityDenied = errors.New("DiscourseConnect identity is not allowed")
)

type pendingSSO struct {
	ReturnTo  string
	Challenge string
	ExpiresAt time.Time
}

// SSOProvider implements the DiscourseConnect browser round-trip. It never
// trusts profile fields from the SSO payload; the returned Discourse user ID
// is immediately re-read through the signed identity bridge.
type SSOProvider struct {
	baseURL     *url.URL
	callbackURL *url.URL
	secret      []byte
	now         func() time.Time
	window      time.Duration
	mu          sync.Mutex
	pending     map[string]pendingSSO
}

func NewSSOProvider(discourseURL, callbackURL string, secret []byte, window time.Duration) (*SSOProvider, error) {
	if len(secret) == 0 {
		return nil, errors.New("DiscourseConnect SSO secret must not be empty")
	}
	base, err := parseHTTPSBase(discourseURL)
	if err != nil {
		return nil, fmt.Errorf("DiscourseConnect URL: %w", err)
	}
	callback, err := parseHTTPSAbsolute(callbackURL)
	if err != nil {
		return nil, fmt.Errorf("DiscourseConnect callback URL: %w", err)
	}
	if window <= 0 {
		window = 10 * time.Minute
	}
	return &SSOProvider{
		baseURL: base, callbackURL: callback, secret: append([]byte(nil), secret...),
		now: time.Now, window: window, pending: make(map[string]pendingSSO),
	}, nil
}

// Begin creates a signed DiscourseConnect request. returnTo is always a
// relative Portal path, so the callback cannot be turned into an open redirect.
func (p *SSOProvider) Begin(_ context.Context, returnTo, challenge string) (string, error) {
	if p == nil || p.baseURL == nil || p.callbackURL == nil {
		return "", errors.New("DiscourseConnect SSO provider is not initialized")
	}
	if err := validateRelativeReturnTo(returnTo); err != nil {
		return "", err
	}
	nonce, err := randomNonce()
	if err != nil {
		return "", err
	}
	now := p.now().UTC()
	p.mu.Lock()
	p.cleanupLocked(now)
	p.pending[nonce] = pendingSSO{ReturnTo: returnTo, Challenge: strings.TrimSpace(challenge), ExpiresAt: now.Add(p.window)}
	p.mu.Unlock()

	callback := *p.callbackURL
	query := callback.Query()
	query.Set("state", nonce)
	callback.RawQuery = query.Encode()
	payload := url.Values{}
	payload.Set("nonce", nonce)
	payload.Set("return_sso_url", callback.String())
	raw := base64.StdEncoding.EncodeToString([]byte(payload.Encode()))
	signature := hmacHex(p.secret, raw)
	provider := *p.baseURL
	provider.Path = strings.TrimRight(provider.Path, "/") + "/session/sso"
	provider.RawQuery = url.Values{"sso": []string{raw}, "sig": []string{signature}}.Encode()
	return provider.String(), nil
}

type SSOIdentity struct {
	DiscourseID int64
	Challenge   string
	ReturnTo    string
}

func (p *SSOProvider) Complete(_ context.Context, request *http.Request) (SSOIdentity, error) {
	if p == nil || request == nil || p.callbackURL == nil {
		return SSOIdentity{}, ErrInvalidSSO
	}
	raw := strings.TrimSpace(request.URL.Query().Get("sso"))
	signature := strings.TrimSpace(request.URL.Query().Get("sig"))
	state := strings.TrimSpace(request.URL.Query().Get("state"))
	if raw == "" || signature == "" || state == "" || !secureHexEqual(hmacHex(p.secret, raw), signature) {
		return SSOIdentity{}, ErrInvalidSSO
	}
	decoded, err := decodeSSOPayload(raw)
	if err != nil {
		return SSOIdentity{}, ErrInvalidSSO
	}
	nonce := strings.TrimSpace(decoded.Get("nonce"))
	returnURL := strings.TrimSpace(decoded.Get("return_sso_url"))
	if nonce == "" || nonce != state || !sameCallbackURL(returnURL, p.callbackURL) {
		return SSOIdentity{}, ErrInvalidSSO
	}
	externalID, err := strconv.ParseInt(strings.TrimSpace(decoded.Get("external_id")), 10, 64)
	if err != nil || externalID <= 0 {
		return SSOIdentity{}, ErrInvalidSSO
	}
	now := p.now().UTC()
	p.mu.Lock()
	p.cleanupLocked(now)
	pending, ok := p.pending[nonce]
	if ok {
		delete(p.pending, nonce)
	}
	p.mu.Unlock()
	if !ok {
		return SSOIdentity{}, ErrSSOStateReplay
	}
	if !pending.ExpiresAt.After(now) {
		return SSOIdentity{}, ErrSSOStateExpired
	}
	return SSOIdentity{DiscourseID: externalID, Challenge: pending.Challenge, ReturnTo: pending.ReturnTo}, nil
}

// EnsureShadowUser creates the internal, non-guessable Connect subject on the
// first login and refreshes the public profile snapshot from the provider.
func EnsureShadowUser(ctx context.Context, users store.UserRepository, provider Provider, discourseID int64) (domain.User, error) {
	if users == nil || provider == nil || discourseID <= 0 {
		return domain.User{}, errors.New("identity mapping is not initialized")
	}
	snapshot, err := provider.FetchUser(ctx, discourseID)
	if err != nil {
		return domain.User{}, err
	}
	if snapshot.DiscourseID != discourseID || strings.TrimSpace(snapshot.Username) == "" {
		return domain.User{}, ErrInvalidSSO
	}
	user, err := users.GetByDiscourseID(ctx, discourseID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return domain.User{}, err
	}
	if errors.Is(err, store.ErrNotFound) {
		subject, subjectErr := newSubject()
		if subjectErr != nil {
			return domain.User{}, subjectErr
		}
		now := time.Now().UTC()
		user = domain.User{Subject: domain.UserID(subject), DiscourseID: discourseID, CreatedAt: now}
	}
	if snapshot.Subject != "" && snapshot.Subject != string(user.Subject) {
		return domain.User{}, ErrIdentityMismatch
	}
	now := time.Now().UTC()
	user.DiscourseID, user.Username, user.Name, user.AvatarURL = snapshot.DiscourseID, snapshot.Username, snapshot.Name, snapshot.AvatarURL
	user.TrustLevel, user.Active, user.Silenced, user.Suspended = snapshot.TrustLevel, snapshot.Active, snapshot.Silenced, snapshot.Suspended
	user.Reviewer, user.Admin, user.UpdatedAt = snapshot.Reviewer, snapshot.Admin, now
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if err := users.Upsert(ctx, user); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func decodeSSOPayload(raw string) (url.Values, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		decoded, err := encoding.DecodeString(raw)
		if err != nil {
			continue
		}
		values, err := url.ParseQuery(string(decoded))
		if err == nil {
			return values, nil
		}
	}
	return nil, ErrInvalidSSO
}

func parseHTTPSBase(raw string) (*url.URL, error) {
	parsed, err := parseHTTPSAbsolute(raw)
	if err != nil {
		return nil, err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed, nil
}

func parseHTTPSAbsolute(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("must be an absolute HTTPS URL")
	}
	return parsed, nil
}

func sameCallbackURL(raw string, expected *url.URL) bool {
	parsed, err := parseHTTPSAbsolute(raw)
	if err != nil {
		return false
	}
	return parsed.String() == expected.String() || (parsed.Path == expected.Path && parsed.Scheme == expected.Scheme && parsed.Host == expected.Host)
}

func validateRelativeReturnTo(raw string) error {
	if raw == "" {
		return errors.New("return path is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") {
		return errors.New("return path must be relative")
	}
	return nil
}

func (p *SSOProvider) cleanupLocked(now time.Time) {
	for nonce, pending := range p.pending {
		if !pending.ExpiresAt.After(now) {
			delete(p.pending, nonce)
		}
	}
}

func newSubject() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "usr_" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func hmacHex(secret []byte, value string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}

func secureHexEqual(expected, provided string) bool {
	if len(provided) != sha256.Size*2 {
		return false
	}
	expectedBytes, err1 := hex.DecodeString(expected)
	providedBytes, err2 := hex.DecodeString(provided)
	return err1 == nil && err2 == nil && hmac.Equal(expectedBytes, providedBytes)
}
