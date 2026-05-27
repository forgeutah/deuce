// Package web serves the embedded Vite SPA build.
//
// Callers must populate server/internal/web/dist/ before running `go build`.
// The repo-root Dockerfile is the production populator: stage 1 runs
// `npm run build` and stage 2 copies the output into this package. For local
// development, `make embed-dist` (from server/) runs the same flow.
//
// Without populated dist contents, `go build` fails with an embed error like
// "pattern dist: no matching files found". A committed .gitkeep keeps the
// directory present for the embed directive; the real build artifacts are
// gitignored.
package web
