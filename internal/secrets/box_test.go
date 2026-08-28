package secrets

import (
	"bytes"
	"testing"
)

func TestBoxEncryptsAndAuthenticatesCiphertext(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	box, err := NewBox(key)
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	ciphertext, err := box.Encrypt("client-secret", "app_1:v1")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Contains([]byte(ciphertext), []byte("client-secret")) {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := box.Decrypt(ciphertext, "app_1:v1")
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if plaintext != "client-secret" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := box.Decrypt(ciphertext, "different-ad"); err == nil {
		t.Fatal("Decrypt() with different associated data succeeded")
	}
}

func TestBoxRejectsInvalidKeyAndTampering(t *testing.T) {
	if _, err := NewBox([]byte("short")); err == nil {
		t.Fatal("NewBox() error = nil for short key")
	}
	box, err := NewBox([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	ciphertext, err := box.Encrypt("secret", "app_1:v1")
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	tampered := ciphertext[:len(ciphertext)-1] + "x"
	if _, err := box.Decrypt(tampered, "app_1:v1"); err == nil {
		t.Fatal("Decrypt() accepted tampered ciphertext")
	}
}
