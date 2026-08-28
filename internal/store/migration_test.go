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
