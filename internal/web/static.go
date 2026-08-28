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
type asset struct {
	name        string
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
		index["/"+entry.Name()] = asset{
			name:        entry.Name(),
			contentType: "text/css; charset=utf-8",
			etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
			body:        body,
		}
	}
	return index
}()

// AssetsPath is the URL prefix under which the embedded stylesheets are
// served, e.g. AssetsPath + "/portal.css" inside template link tags.
const AssetsPath = "/connect/assets"

// RegisterAssetRoutes serves the embedded portal stylesheets with strong
// ETags. The files are compiled into the binary, so a content change always
// ships with a new build and a new ETag; browsers may cache aggressively
// between deploys.
func RegisterAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc(AssetsPath+"/", serveAsset)
}

func serveAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	item, ok := assetIndex[strings.TrimPrefix(r.URL.Path, AssetsPath)]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", item.contentType)
	w.Header().Set("Cache-Control", "public, max-age=604800")
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
