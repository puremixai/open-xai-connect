package assets

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStorePersistsAndServesCanonicalLogo(t *testing.T) {
	root := t.TempDir()
	store, err := NewFileStore(root)
	if err != nil {
		t.Fatal(err)
	}
	var encoded strings.Builder
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 80, B: 180, A: 255})
		}
	}
	if err := png.Encode(&encodedWriter{Builder: &encoded}, img); err != nil {
		t.Fatal(err)
	}
	path, err := store.Save(context.Background(), "usr_1", []byte(encoded.String()), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://connect.example"+path, nil)
	response := httptest.NewRecorder()
	store.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status/content-type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
	if len(response.Body.Bytes()) == 0 {
		t.Fatal("served asset is empty")
	}
}

func TestFileStoreRejectsTraversalAndUnsupportedNames(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"/assets/../secret.png", "/assets/secret.svg", "/assets/.png"} {
		request := httptest.NewRequest(http.MethodGet, "https://connect.example"+value, nil)
		response := httptest.NewRecorder()
		store.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d", value, response.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(store.root, "secret.png")); !os.IsNotExist(err) {
		t.Fatalf("unexpected path created: %v", err)
	}
}

type encodedWriter struct{ *strings.Builder }

func (w *encodedWriter) Write(p []byte) (int, error) { return w.Builder.WriteString(string(p)) }
