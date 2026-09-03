package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"path"
	"strconv"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

// asset describes one embedded portal asset served under /connect/assets/.
// Assets are content-addressed: AssetURL returns a fingerprinted path (for
// example /connect/assets/portal.1b2f90ca.css) that changes whenever the file
// content changes, so those URLs can be cached indefinitely. The plain name
// keeps working as a short-lived compatibility alias for HTML that is still in
// flight from a deploy.
type asset struct {
	name        string
	version     string
	contentType string
	etag        string
	body        []byte
}

var assetIndex = func() map[string]asset {
	index := map[string]asset{}
	entries, err := staticFS.ReadDir("static")
	if err != nil {
		return index
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		contentType, ok := assetContentType(entry.Name())
		if !ok {
			continue
		}
		body, err := staticFS.ReadFile(path.Join("static", entry.Name()))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		version := hex.EncodeToString(sum[:4])
		item := asset{
			name:        entry.Name(),
			version:     version,
			contentType: contentType,
			etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
			body:        body,
		}
		ext := path.Ext(entry.Name())
		base := strings.TrimSuffix(entry.Name(), ext)
		index["/"+entry.Name()] = item
		index["/"+base+"."+version+ext] = item
	}
	return index
}()

func assetContentType(name string) (string, bool) {
	switch strings.ToLower(path.Ext(name)) {
	case ".css":
		return "text/css; charset=utf-8", true
	case ".svg":
		return "image/svg+xml", true
	default:
		return "", false
	}
}

// AssetsPath is the URL prefix under which embedded portal assets are served,
// e.g. AssetsPath + "/portal.css" inside template link tags.
const AssetsPath = "/connect/assets"

// AssetURL returns the cache-busting URL of an embedded asset, e.g.
// AssetURL("portal.css") resolves to /connect/assets/portal.<version>.css.
// Unknown names fall back to the legacy path so the failure mode is a plain
// 404 instead of a broken template.
func AssetURL(name string) string {
	item, ok := assetIndex["/"+path.Base(name)]
	if !ok {
		return AssetsPath + "/" + path.Base(name)
	}
	ext := path.Ext(item.name)
	base := strings.TrimSuffix(item.name, ext)
	return AssetsPath + "/" + base + "." + item.version + ext
}

// RegisterAssetRoutes serves the embedded portal assets. Fingerprinted URLs
// are immutable (the content hash is part of the path), so they carry a
// one-year lifetime; the legacy alias stays short-lived so stale references
// heal quickly instead of pinning an old asset for a week.
func RegisterAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc(AssetsPath+"/", serveAsset)
}

func serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requested := strings.TrimPrefix(r.URL.Path, AssetsPath)
	item, ok := assetIndex[requested]
	if !ok {
		http.NotFound(w, r)
		return
	}
	cacheControl := "public, max-age=31536000, immutable"
	if requested == "/"+item.name {
		cacheControl = "public, max-age=300, must-revalidate"
	}
	w.Header().Set("Content-Type", item.contentType)
	w.Header().Set("Cache-Control", cacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", item.etag)
	if r.Header.Get("If-None-Match") == item.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(item.body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(item.body)
}
