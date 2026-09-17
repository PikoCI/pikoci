package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/pikoci/pikoci/pikoci/transport/http/assets"
)

// Static assets are served from an embed.FS, which carries no modification
// times. http.FileServer therefore sends them with no Last-Modified, no ETag
// and no Cache-Control, and a browser with no freshness information falls back
// to its own heuristics. Safari's are the most generous: it keeps reusing a
// cached module on ordinary navigation without asking the server, so after a
// deploy it runs the previous build's JS until the user empties the cache.
//
// The fix is to make every build's assets live at a URL no earlier build ever
// used. The whole tree is mounted under /assets/<build>/, and the page
// references it from there; the module graph's relative imports resolve inside
// the prefix on their own, so the JS files do not change. A URL that can never
// mean anything else can be cached forever, hence immutable. The HTML that
// points at the current prefix is the one thing that must always be re-read.
//
// The bare /js/, /css/, /images/ and /fonts/ routes stay for anything that
// links them directly (the logo in the UI, older bookmarks); they revalidate on
// every use rather than pinning a stale copy.

// assetPrefix returns the versioned mount point for this build's assets, as a
// path with leading and trailing slash. A binary built without ldflags has no
// commit to key on, so a start timestamp stands in: the cache is then busted
// on every restart, which is what a dev loop wants anyway.
func assetPrefix(commit string) string {
	if commit == "" || commit == "unknown" {
		commit = strconv.FormatInt(time.Now().Unix(), 10)
	}
	return "/assets/" + commit + "/"
}

// mountAssets registers the embedded asset tree on r twice: immutable under
// prefix, and revalidate-always at the bare top-level paths.
func mountAssets(r *mux.Router, prefix string) {
	fs := http.FileServer(http.FS(assets.Assets))

	r.PathPrefix(prefix).Handler(http.StripPrefix(prefix, cacheControl("public, max-age=31536000, immutable", fs)))

	legacy := cacheControl("no-cache", fs)
	for _, p := range []string{"/css/", "/js/", "/images/", "/fonts/"} {
		r.PathPrefix(p).Handler(legacy)
	}
}

// cacheControl sets the Cache-Control header before delegating to next.
func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}
