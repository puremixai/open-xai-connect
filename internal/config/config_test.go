package config

import (
	"encoding/base64"
	"os"
	"testing"
)

func setValidEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("CONNECT_LISTEN_ADDR", ":8080")
	t.Setenv("CONNECT_ISSUER_URL", "https://connect.example")
	t.Setenv("DISCOURSE_URL", "https://forum.example")
	t.Setenv("DISCOURSE_SHARED_SECRET", "shared-secret")
	t.Setenv("POSTGRES_DSN", "postgres://connect:password@postgres/connect")
	t.Setenv("REDIS_URL", "redis://redis:6379/0")
	t.Setenv("HYDRA_PUBLIC_URL", "http://hydra:4444")
	t.Setenv("HYDRA_ADMIN_URL", "http://hydra:4445")
	t.Setenv("CONNECT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901")))
	t.Setenv("CONNECT_COOKIE_SECURE", "true")
	t.Setenv("CONNECT_COOKIE_NAME", "connect_session")
	t.Setenv("CONNECT_ENV", "test")
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CONNECT_LISTEN_ADDR", "CONNECT_ISSUER_URL", "DISCOURSE_URL",
		"DISCOURSE_SHARED_SECRET", "POSTGRES_DSN", "REDIS_URL",
		"HYDRA_PUBLIC_URL", "HYDRA_ADMIN_URL", "CONNECT_ENCRYPTION_KEY",
		"CONNECT_COOKIE_SECURE", "CONNECT_COOKIE_NAME", "CONNECT_ENV",
	} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
}

func TestLoadRejectsMissingRequiredEnvironment(t *testing.T) {
	clearConfigEnvironment(t)

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want missing environment error")
	}
}

func TestLoadRejectsEncryptionKeyWithWrongLength(t *testing.T) {
	setValidEnvironment(t)
	t.Setenv("CONNECT_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want encryption key length error")
	}
}

func TestLoadReturnsValidatedConfiguration(t *testing.T) {
	setValidEnvironment(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ListenAddr != ":8080" {
		t.Fatalf("ListenAddr = %q, want %q", got.ListenAddr, ":8080")
	}
	if got.PublicIssuerURL != "https://connect.example" {
		t.Fatalf("PublicIssuerURL = %q", got.PublicIssuerURL)
	}
	if len(got.EncryptionKey) != 32 {
		t.Fatalf("EncryptionKey length = %d, want 32", len(got.EncryptionKey))
	}
	if !got.CookieSecure || got.CookieName != "connect_session" {
		t.Fatalf("cookie settings = secure:%v name:%q", got.CookieSecure, got.CookieName)
	}
}
