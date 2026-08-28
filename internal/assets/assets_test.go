package assets

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 32, G: 96, B: 192, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func TestValidateLogoChecksRealMIMEAndDimensions(t *testing.T) {
	info, err := ValidateLogo(pngBytes(t, 128, 128), "image/png")
	if err != nil {
		t.Fatalf("ValidateLogo() error = %v", err)
	}
	if info.MIME != "image/png" || info.Width != 128 || info.Height != 128 {
		t.Fatalf("info = %#v", info)
	}
	if _, err := ValidateLogo(pngBytes(t, 8, 8), "image/png"); err == nil {
		t.Fatal("ValidateLogo() accepted logo smaller than minimum dimensions")
	}
}

func TestValidateLogoRejectsMIMEMismatchSVGAndOversize(t *testing.T) {
	data := pngBytes(t, 64, 64)
	if _, err := ValidateLogo(data, "image/jpeg"); err == nil {
		t.Fatal("ValidateLogo() accepted MIME mismatch")
	}
	if _, err := ValidateLogo([]byte("<svg></svg>"), "image/svg+xml"); err == nil {
		t.Fatal("ValidateLogo() accepted SVG")
	}
	oversized := make([]byte, maxLogoBytes+1)
	if _, err := ValidateLogo(oversized, "image/png"); err == nil {
		t.Fatal("ValidateLogo() accepted oversized file")
	}
}

func TestMemoryAssetStoreReturnsConnectHostedURL(t *testing.T) {
	store := NewMemoryStore()
	url, err := store.Save(context.Background(), "sub_1", pngBytes(t, 64, 64), "image/png")
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if !strings.HasPrefix(url, "/assets/") || strings.Contains(url, "..") {
		t.Fatalf("asset URL = %q", url)
	}
}
