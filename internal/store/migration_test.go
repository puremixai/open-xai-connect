package store

import (
	"os"
	"strings"
	"testing"
)

func TestInitialMigrationContainsRequiredTables(t *testing.T) {
	sql, err := os.ReadFile("../../migrations/001_init.sql")
	if err != nil {
		t.Fatalf("read initial migration: %v", err)
	}
	content := strings.ToLower(string(sql))
	for _, table := range []string{"users", "applications", "application_callbacks", "verified_domains", "consents", "outbox_events", "audit_events"} {
		if !strings.Contains(content, "create table "+table) {
			t.Fatalf("migration does not create table %s", table)
		}
	}
}

func TestEmailMigrationAddsUserEmail(t *testing.T) {
	sql, err := os.ReadFile("../../migrations/002_user_email.sql")
	if err != nil {
		t.Fatalf("read email migration: %v", err)
	}
	content := strings.ToLower(string(sql))
	if !strings.Contains(content, "alter table users") || !strings.Contains(content, "add column") || !strings.Contains(content, "email") {
		t.Fatalf("email migration does not add users.email: %s", content)
	}
}

func TestApplicationSecurityMigrationAddsPKCENonceSetting(t *testing.T) {
	sql, err := os.ReadFile("../../migrations/003_application_pkce_nonce.sql")
	if err != nil {
		t.Fatalf("read PKCE/nonce migration: %v", err)
	}
	content := strings.ToLower(string(sql))
	if !strings.Contains(content, "alter table applications") ||
		!strings.Contains(content, "require_pkce_nonce") ||
		!strings.Contains(content, "default true") {
		t.Fatalf("PKCE/nonce migration does not add a default-enabled application setting: %s", content)
	}
}
