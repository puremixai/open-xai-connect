package identity

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"connect.xai.run/internal/domain"
	"connect.xai.run/internal/store"
	"connect.xai.run/internal/store/memory"
)

func TestSSOProviderRoundTripAndReplayProtection(t *testing.T) {
	provider, err := NewSSOProvider("https://forum.example", "https://connect.example/connect/callback", []byte("shared-secret"), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	location, err := provider.Begin(context.Background(), "/connect/apps", "")
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := url.Parse(location)
	if err != nil {
		t.Fatal(err)
	}
	rawSSO := redirect.Query().Get("sso")
	signature := redirect.Query().Get("sig")
	decoded, err := base64.StdEncoding.DecodeString(rawSSO)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := url.ParseQuery(string(decoded))
	if err != nil {
		t.Fatal(err)
	}
	nonce := payload.Get("nonce")
	if nonce == "" {
		t.Fatal("missing nonce")
	}
	callback, err := url.Parse(payload.Get("return_sso_url"))
	if err != nil {
		t.Fatal(err)
	}
	query := callback.Query()
	query.Set("sso", base64.StdEncoding.EncodeToString([]byte(url.Values{
		"nonce":          []string{nonce},
		"return_sso_url": []string{payload.Get("return_sso_url")},
		"external_id":    []string{"42"},
	}.Encode())))
	query.Set("sig", hmacHex([]byte("shared-secret"), query.Get("sso")))
	request := httptest.NewRequest("GET", "/connect/callback?"+query.Encode(), nil)
	identity, err := provider.Complete(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if identity.DiscourseID != 42 || identity.ReturnTo != "/connect/apps" {
		t.Fatalf("identity = %#v", identity)
	}
	if _, err := provider.Complete(context.Background(), request); err != ErrSSOStateReplay {
		t.Fatalf("replay error = %v", err)
	}
	_ = signature // signature is checked by the provider in the callback above.
}

func TestSSOProviderRejectsExternalReturnPath(t *testing.T) {
	provider, err := NewSSOProvider("https://forum.example", "https://connect.example/connect/callback", []byte("shared-secret"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Begin(context.Background(), "https://attacker.example", ""); err == nil {
		t.Fatal("expected absolute return path to be rejected")
	}
}

type ssoTestProvider struct {
	snapshot UserSnapshot
}

func (p ssoTestProvider) FetchUser(_ context.Context, id int64) (UserSnapshot, error) {
	if p.snapshot.DiscourseID != id {
		return UserSnapshot{}, store.ErrNotFound
	}
	return p.snapshot, nil
}

func TestEnsureShadowUserKeepsStableSubject(t *testing.T) {
	users := memory.NewUserRepository()
	provider := ssoTestProvider{snapshot: UserSnapshot{DiscourseID: 7, Username: "alice", Name: "Alice", TrustLevel: 1, Active: true}}
	first, err := EnsureShadowUser(context.Background(), users, provider, 7)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(first.Subject), "usr_") || first.Subject == domain.UserID("7") {
		t.Fatalf("subject = %q", first.Subject)
	}
	provider.snapshot.Name = "Alice Updated"
	second, err := EnsureShadowUser(context.Background(), users, provider, 7)
	if err != nil {
		t.Fatal(err)
	}
	if second.Subject != first.Subject || second.Name != "Alice Updated" {
		t.Fatalf("shadow user changed unexpectedly: first=%#v second=%#v", first, second)
	}
}
