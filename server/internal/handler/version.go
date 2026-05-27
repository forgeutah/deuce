package handler

import "net/http"

// Version returns a handler that responds with {"version": "<version>"} as
// JSON. The version is captured in the closure rather than stored as a
// package global so it stays scoped to whoever constructs the route.
//
// Set at build time via -ldflags="-X main.Version=v1.2.3" (see the
// release-build Makefile target and the Dockerfile). Defaults to "dev"
// when built without ldflags.
func Version(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": version})
	}
}
