package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

type Box struct {
	aead cipher.AEAD
}

func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Encrypt(plaintext, associatedData string) (string, error) {
	if b == nil || b.aead == nil {
		return "", errors.New("encryption box is not initialized")
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate encryption nonce: %w", err)
	}
	ciphertext := b.aead.Seal(nonce, nonce, []byte(plaintext), []byte(associatedData))
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func (b *Box) Decrypt(ciphertext, associatedData string) (string, error) {
	if b == nil || b.aead == nil {
		return "", errors.New("encryption box is not initialized")
	}
	raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", errors.New("invalid encrypted value")
	}
	nonceSize := b.aead.NonceSize()
	if len(raw) <= nonceSize {
		return "", errors.New("invalid encrypted value")
	}
	plaintext, err := b.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], []byte(associatedData))
	if err != nil {
		return "", errors.New("encrypted value authentication failed")
	}
	return string(plaintext), nil
}
