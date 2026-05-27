package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves files from the embedded Vite
// build, falling back to index.html for unknown paths (SPA semantics) while
// still returning 404 for missing hashed assets under /assets/.
//
// Caching:
//   - /assets/* (Vite emits hashed filenames): Cache-Control immutable, 1 year
//   - everything else (including index.html): Cache-Control no-cache
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// fs.Sub only fails if "dist" isn't valid — impossible given the
		// embed directive above. Panic on misconfiguration at process start
		// rather than serving a misleading 500 per request.
		panic("web: embedded dist filesystem is invalid: " + err.Error())
	}
	return handlerFromFS(sub)
}

// handlerFromFS builds the SPA handler against an arbitrary filesystem so
// unit tests can exercise the routing/caching logic against a fixture.
func handlerFromFS(sub fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := path.Clean(r.URL.Path)
		reqPath = strings.TrimPrefix(reqPath, "/")
		if reqPath == "" || reqPath == "." {
			reqPath = "index.html"
		}

		// SPA fallback: if the requested path doesn't exist in the embedded FS,
		// rewrite to / so the FileServer returns index.html — UNLESS the path
		// is under /assets/, where a missing file should 404 cleanly (a
		// hashed-asset miss is a real error, not a route to the SPA shell).
		if _, err := fs.Stat(sub, reqPath); err != nil {
			if strings.HasPrefix(reqPath, "assets/") {
				http.NotFound(w, r)
				return
			}
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			setCacheHeader(w, "/")
			fileServer.ServeHTTP(w, r2)
			return
		}

		setCacheHeader(w, r.URL.Path)
		fileServer.ServeHTTP(w, r)
	})
}

func setCacheHeader(w http.ResponseWriter, urlPath string) {
	if strings.HasPrefix(urlPath, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}
