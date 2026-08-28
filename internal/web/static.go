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

//go:embed static/*.css
var staticFS embed.FS

// asset describes one embedded stylesheet served under /connect/assets/.
// Stylesheets are content-addressed: AssetURL returns a fingerprinted path
// (for example /connect/assets/portal.1b2f90ca.css) that changes whenever the
// file content changes, so those URLs can be cached indefinitely. The plain
// name keeps working as a short-lived compatibility alias for HTML that is
// still in flight from a deploy.
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
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".css") {
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
			contentType: "text/css; charset=utf-8",
			etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
			body:        body,
		}
		base := strings.TrimSuffix(entry.Name(), ".css")
		index["/"+entry.Name()] = item
		index["/"+base+"."+version+".css"] = item
	}
	return index
}()

// AssetsPath is the URL prefix under which the embedded stylesheets are
// served, e.g. AssetsPath + "/portal.css" inside template link tags.
const AssetsPath = "/connect/assets"

// AssetURL returns the cache-busting URL of an embedded stylesheet, e.g.
// AssetURL("portal.css") resolves to /connect/assets/portal.<version>.css.
// Unknown names fall back to the legacy path so the failure mode is a plain
// 404 instead of a broken template.
func AssetURL(name string) string {
	item, ok := assetIndex["/"+path.Base(name)]
	if !ok {
		return AssetsPath + "/" + path.Base(name)
	}
	base := strings.TrimSuffix(item.name, ".css")
	return AssetsPath + "/" + base + "." + item.version + ".css"
}

// RegisterAssetRoutes serves the embedded portal stylesheets. Fingerprinted
// URLs are immutable (the content hash is part of the path), so they carry a
// one-year lifetime; the legacy alias stays short-lived so stale references
// heal quickly instead of pinning an old stylesheet for a week.
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
