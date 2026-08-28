package assets

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileStore persists canonical logo bytes outside the database. Files are
// addressed by random IDs and served with a fixed MIME type; user-controlled
// paths are never passed directly to the filesystem.
type FileStore struct {
	root string
}

func NewFileStore(root string) (*FileStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("asset directory is required")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &FileStore{root: root}, nil
}

func (s *FileStore) Save(ctx context.Context, owner string, data []byte, mime string) (string, error) {
	if s == nil || s.root == "" {
		return "", errors.New("asset store is not initialized")
	}
	return saveFile(ctx, s.root, owner, data, mime)
}

func (s *FileStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.root == "" {
		http.Error(w, "asset store unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if !validAssetName(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.root, filepath.Base(name))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	mime := "image/png"
	if strings.HasSuffix(name, ".jpg") {
		mime = "image/jpeg"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, filepath.Base(name), fileModTime(path), bytes.NewReader(data))
}

func saveFile(ctx context.Context, root, owner string, data []byte, mime string) (string, error) {
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
	id, err := randomAssetID()
	if err != nil {
		return "", err
	}
	extension := ".png"
	if info.MIME == "image/jpeg" {
		extension = ".jpg"
	}
	name := id + extension
	destination := filepath.Join(root, name)
	tmp, err := os.CreateTemp(root, ".upload-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(canonical); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return "", err
	}
	return "/assets/" + name, nil
}

func validAssetName(name string) bool {
	if name == "" || filepath.Base(name) != name {
		return false
	}
	if strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg") {
		name = name[:len(name)-4]
		if name == "" {
			return false
		}
		for _, r := range name {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
				return false
			}
		}
		return true
	}
	return false
}

func fileModTime(path string) (modTime time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Unix(0, 0)
	}
	return info.ModTime()
}

func randomAssetID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
