package assets

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
)

type Asset struct {
	ID      string
	Owner   string
	MIME    string
	Content []byte
}

type MemoryStore struct {
	mu    sync.RWMutex
	files map[string]Asset
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{files: make(map[string]Asset)}
}

func (s *MemoryStore) Save(ctx context.Context, owner string, data []byte, mime string) (string, error) {
	if err := contextError(ctx); err != nil {
		return "", err
	}
	if owner == "" {
		return "", errors.New("asset owner is required")
	}
	info, err := ValidateLogo(data, mime)
	if err != nil {
		return "", err
	}
	canonical, err := encodeCanonical(data, info.MIME)
	if err != nil {
		return "", err
	}
	rawID := make([]byte, 18)
	if _, err := rand.Read(rawID); err != nil {
		return "", err
	}
	id := base64.RawURLEncoding.EncodeToString(rawID)
	asset := Asset{ID: id, Owner: owner, MIME: info.MIME, Content: append([]byte(nil), canonical...)}
	s.mu.Lock()
	s.files[id] = asset
	s.mu.Unlock()
	extension := "png"
	if info.MIME == "image/jpeg" {
		extension = "jpg"
	}
	return "/assets/" + id + "." + extension, nil
}

func (s *MemoryStore) Get(id string) (Asset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	asset, ok := s.files[id]
	if !ok {
		return Asset{}, errors.New("asset not found")
	}
	asset.Content = append([]byte(nil), asset.Content...)
	return asset, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
