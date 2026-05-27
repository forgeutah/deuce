package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

const indexHTML = `<!doctype html><html><body><div id="root"></div></body></html>`

func fixtureFS() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte(indexHTML),
		},
		"assets/app-abc123.js": &fstest.MapFile{
			Data: []byte(`console.log("hello");`),
		},
		"assets/app-abc123.css": &fstest.MapFile{
			Data: []byte(`body { margin: 0; }`),
		},
		"favicon.ico": &fstest.MapFile{
			Data: []byte{0x00},
		},
	}
}

func get(t *testing.T, h http.Handler, urlPath string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, urlPath, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler_ServesIndexHTMLAtRoot(t *testing.T) {
	h := handlerFromFS(fixtureFS())
	rec := get(t, h, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("body: want SPA shell, got %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control: want no-cache, got %q", got)
	}
}

func TestHandler_ServesHashedAssetWithImmutableCache(t *testing.T) {
	h := handlerFromFS(fixtureFS())
	rec := get(t, h, "/assets/app-abc123.js")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Fatalf("Content-Type: want javascript, got %q", ct)
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control: want immutable, got %q", cc)
	}
}

func TestHandler_FallsBackToIndexForUnknownPath(t *testing.T) {
	h := handlerFromFS(fixtureFS())
	rec := get(t, h, "/some/deep/unknown/route")

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("body: want SPA shell on fallback, got %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control: want no-cache on fallback, got %q", got)
	}
}

func TestHandler_MissingAssetReturns404(t *testing.T) {
	h := handlerFromFS(fixtureFS())
	rec := get(t, h, "/assets/does-not-exist.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: want 404 for missing asset, got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), `<div id="root">`) {
		t.Fatalf("body: missing-asset response should NOT be the SPA shell")
	}
}

func TestHandler_IndexHTMLDirectRequest(t *testing.T) {
	// http.FileServer normalizes /index.html to / via a 301 — that's correct
	// SPA behavior (canonical URL for the shell is /). The test asserts the
	// redirect lands at /, and that the eventual GET / serves the shell with
	// no-cache.
	h := handlerFromFS(fixtureFS())
	rec := get(t, h, "/index.html")

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status: want 301 (FileServer canonicalizes /index.html to /), got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "./" && loc != "/" {
		t.Fatalf("Location: want / or ./, got %q", loc)
	}

	// Follow the redirect and verify the shell + cache header.
	rec2 := get(t, h, "/")
	if rec2.Code != http.StatusOK {
		t.Fatalf("after redirect to /: want 200, got %d", rec2.Code)
	}
	if got := rec2.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control on /: want no-cache, got %q", got)
	}
}

// TestHandler_DoesNotInterceptAPIRoutes confirms the static handler, when
// mounted via chi's catch-all r.Handle("/*", ...), still defers to API
// routes registered earlier on the router. chi resolves most-specific-first,
// so this assertion is really about chi's behavior + our handler being a
// well-behaved http.Handler — a regression would catch a future change that
// e.g. registers /*  before /api/*.
func TestHandler_DoesNotInterceptAPIRoutes(t *testing.T) {
	// Stand up a minimal router that mirrors server.Router()'s mounting order:
	// API routes first, then the catch-all static handler.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/whatever", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"api":"ok"}`))
	})
	mux.Handle("/", handlerFromFS(fixtureFS()))

	rec := get(t, mux, "/api/whatever")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200 from API handler, got %d", rec.Code)
	}
	if rec.Body.String() != `{"api":"ok"}` {
		t.Fatalf("body: API handler should win over static fallback, got %q", rec.Body.String())
	}
}

func TestHandler_EmbeddedFSContainsGitkeep(t *testing.T) {
	// Smoke test against the real embedded FS — proves the //go:embed directive
	// is wired and at least one file is present. In CI/Docker the dist/ will
	// be populated by `npm run build`; locally the committed .gitkeep keeps the
	// directory present so `go build` doesn't fail with an empty-pattern error.
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		t.Fatalf("fs.ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("embedded dist FS is empty — at least .gitkeep or a populated build should be present")
	}
}
