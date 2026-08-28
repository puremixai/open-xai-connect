package session

import "testing"

func TestCSRFTokenBindsToSession(t *testing.T) {
	csrf, err := NewCSRF([]byte("csrf-secret-012345"))
	if err != nil {
		t.Fatalf("NewCSRF() error = %v", err)
	}
	token, err := csrf.Token("session-1")
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if !csrf.Verify("session-1", token) {
		t.Fatal("Verify() rejected token for same session")
	}
	if csrf.Verify("session-2", token) || csrf.Verify("session-1", token+"x") {
		t.Fatal("Verify() accepted token for a different session or tampered token")
	}
}

func TestCSRFRejectsEmptySecretAndSession(t *testing.T) {
	if _, err := NewCSRF(nil); err == nil {
		t.Fatal("NewCSRF() accepted empty secret")
	}
	csrf, _ := NewCSRF([]byte("csrf-secret-012345"))
	if _, err := csrf.Token(""); err == nil {
		t.Fatal("Token() accepted empty session")
	}
}
