package identity

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidSignature = errors.New("invalid Connect signature")
	ErrExpiredRequest   = errors.New("Connect request timestamp outside allowed window")
	ErrReplay           = errors.New("Connect request nonce was already used")
)

type NonceStore interface {
	CheckAndStore(context.Context, string, time.Time) (bool, error)
}

type MemoryNonceStore struct {
	mu     sync.Mutex
	values map[string]time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore {
	return &MemoryNonceStore{values: make(map[string]time.Time)}
}

func (s *MemoryNonceStore) CheckAndStore(_ context.Context, nonce string, expiresAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for key, expiry := range s.values {
		if !expiry.After(now) {
			delete(s.values, key)
		}
	}
	if _, exists := s.values[nonce]; exists {
		return false, nil
	}
	s.values[nonce] = expiresAt
	return true, nil
}

type Verifier struct {
	secret  []byte
	maxSkew time.Duration
	nonces  NonceStore
	now     func() time.Time
}

func NewVerifier(secret []byte, maxSkew time.Duration, nonces NonceStore) (*Verifier, error) {
	if len(secret) == 0 {
		return nil, errors.New("Connect shared secret must not be empty")
	}
	if maxSkew <= 0 {
		maxSkew = 5 * time.Minute
	}
	if nonces == nil {
		return nil, errors.New("Connect nonce store is required")
	}
	return &Verifier{
		secret:  append([]byte(nil), secret...),
		maxSkew: maxSkew,
		nonces:  nonces,
		now:     time.Now,
	}, nil
}

func Sign(secret []byte, method, path string, timestamp time.Time, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical(method, path, timestamp, nonce, body)))
	return hex.EncodeToString(mac.Sum(nil))
}

func (v *Verifier) Verify(ctx context.Context, request *http.Request, body []byte) error {
	if request == nil {
		return ErrInvalidSignature
	}
	timestampRaw := strings.TrimSpace(request.Header.Get("X-Connect-Timestamp"))
	nonce := strings.TrimSpace(request.Header.Get("X-Connect-Nonce"))
	signature := strings.TrimSpace(request.Header.Get("X-Connect-Signature"))
	if timestampRaw == "" || nonce == "" || signature == "" || len(nonce) > 128 {
		return ErrInvalidSignature
	}
	timestampSeconds, err := strconv.ParseInt(timestampRaw, 10, 64)
	if err != nil {
		return ErrInvalidSignature
	}
	timestamp := time.Unix(timestampSeconds, 0)
	now := v.now()
	if now.Sub(timestamp) > v.maxSkew || timestamp.Sub(now) > v.maxSkew {
		return ErrExpiredRequest
	}
	expected := Sign(v.secret, request.Method, request.URL.RequestURI(), timestamp, nonce, body)
	provided, err := hex.DecodeString(signature)
	if err != nil || len(provided) != sha256.Size ||
		subtle.ConstantTimeCompare(provided, mustDecodeHex(expected)) != 1 {
		return ErrInvalidSignature
	}
	accepted, err := v.nonces.CheckAndStore(ctx, nonce, timestamp.Add(v.maxSkew))
	if err != nil {
		return fmt.Errorf("check Connect nonce: %w", err)
	}
	if !accepted {
		return ErrReplay
	}
	return nil
}

func canonical(method, path string, timestamp time.Time, nonce string, body []byte) string {
	return strings.ToUpper(method) + "\n" + path + "\n" +
		strconv.FormatInt(timestamp.Unix(), 10) + "\n" + nonce + "\n" + string(body)
}

func mustDecodeHex(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}
