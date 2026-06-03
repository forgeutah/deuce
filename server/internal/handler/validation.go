package handler

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// validWorkspaceID matches what we're willing to hand `devpod up --id`,
// `devpod stop`, and `devpod delete` as a workspace ID. Session names flow
// into these CLI argv slots so a name starting with `--` or carrying
// `/`/`:` could mislead devpod's own argument parser, and a name carrying
// shell metacharacters (we don't exec via shell, but defense-in-depth) is
// rejected.
//
// First char: alphanumeric. Subsequent: alphanumeric, dash, underscore.
// Bounded length keeps the resulting docker container names well below
// docker's 255-char ceiling once devpod prefixes its own naming bits.
var validWorkspaceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// ErrInvalidWorkspaceID is returned by validateWorkspaceID when the input
// fails the allowlist. Callers map this to a 400 INVALID_NAME response.
var ErrInvalidWorkspaceID = errors.New("workspace ID must be 1-64 chars, start with alphanumeric, and contain only [a-zA-Z0-9_-]")

func validateWorkspaceID(name string) error {
	if !validWorkspaceID.MatchString(name) {
		return ErrInvalidWorkspaceID
	}
	return nil
}

// ErrInvalidRepoURL is returned by validateRepoURL when the input is not a
// reasonable git-clone target. Callers map this to a 400 INVALID_REPO_URL
// response.
var ErrInvalidRepoURL = errors.New("repo URL must be https://, ssh://, or git@<host>:<path>")

// validateRepoURL accepts the three git-clone shapes devpod actually uses.
// Empty input is also allowed — sessions created without a repo URL skip
// devpod workspace creation entirely (see CreateSession's existing guard).
//
// Rejects:
//   - file:// and bare paths (would let a user create a session that
//     clones from a local filesystem path the server process can read)
//   - URLs with embedded credentials (e.g. https://user:token@host) —
//     these would be re-executed by devpod on every Start/Rebuild
//   - any scheme other than https / ssh / git
func validateRepoURL(raw string) error {
	if raw == "" {
		return nil
	}

	// scp-style ssh: git@host:owner/repo.git — does not parse as URL.
	if strings.HasPrefix(raw, "git@") {
		// Must look like git@<host>:<path>, no embedded creds beyond the
		// leading git@ prefix.
		rest := strings.TrimPrefix(raw, "git@")
		colon := strings.Index(rest, ":")
		if colon <= 0 || colon == len(rest)-1 {
			return ErrInvalidRepoURL
		}
		host := rest[:colon]
		if strings.ContainsAny(host, "/@ \t") {
			return ErrInvalidRepoURL
		}
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidRepoURL
	}

	switch strings.ToLower(u.Scheme) {
	case "https", "ssh":
		// ok
	default:
		return ErrInvalidRepoURL
	}

	if u.Host == "" {
		return ErrInvalidRepoURL
	}

	// Reject embedded credentials. A bare username is allowed on ssh:// URLs
	// (e.g. ssh://git@github.com is a canonical SSH-with-key form, where the
	// `git` user is the well-known login name on hosted git providers and
	// the actual auth is the SSH key). Passwords/tokens in the URL are
	// rejected on both schemes — they'd be re-executed by devpod every
	// Start/Rebuild and end up in process listings and logs.
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			return ErrInvalidRepoURL
		}
		// On HTTPS, a bare username is almost always a token used as
		// HTTP-Basic auth (the GitHub PAT convention) — reject it.
		if strings.EqualFold(u.Scheme, "https") {
			return ErrInvalidRepoURL
		}
	}

	return nil
}
