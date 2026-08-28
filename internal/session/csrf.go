package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

type CSRF struct {
	secret []byte
}

func NewCSRF(secret []byte) (*CSRF, error) {
	if len(secret) < 16 {
		return nil, errors.New("CSRF secret must contain at least 128 bits")
	}
	return &CSRF{secret: append([]byte(nil), secret...)}, nil
}

func (c *CSRF) Token(sessionID string) (string, error) {
	if c == nil || len(c.secret) == 0 {
		return "", errors.New("CSRF signer is not initialized")
	}
	if sessionID == "" {
		return "", errors.New("session ID is required")
	}
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte("connect-csrf-v1\n" + sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (c *CSRF) Verify(sessionID, token string) bool {
	if c == nil || sessionID == "" || token == "" {
		return false
	}
	expected, err := c.Token(sessionID)
	if err != nil {
		return false
	}
	provided, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	expectedBytes, err := base64.RawURLEncoding.DecodeString(expected)
	if err != nil {
		return false
	}
	return hmac.Equal(provided, expectedBytes)
}
