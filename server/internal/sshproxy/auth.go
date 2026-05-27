package sshproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"time"

	"github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/ssh"
)

// usernameRegex requires usernames of the form "dc-<canonical UUID>".
// Matched before any DB lookup so garbage usernames don't load Postgres.
var usernameRegex = regexp.MustCompile(`^dc-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Extension keys carried on every authenticated ssh.Permissions. Named
// generically (no "deuce-" prefix) so the package remains internally
// portable if it's ever extracted as a library. SECURITY: never log the
// full Permissions struct — only fingerprint (`fp`) is safe to log.
const (
	extSessionID = "session-id"
	extUserID    = "user-id"
	extKeyID     = "key-id"
	extFP        = "fp"
)

// publicKeyCallback builds the ssh.ServerConfig.PublicKeyCallback. Closes
// over the server's queries + workspace.Manager so the callback can
// resolve the session-member key and pre-check container reachability.
func (s *Server) publicKeyCallback(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	// 1. Username gate — strict regex, no DB load on garbage input.
	user := meta.User()
	if !usernameRegex.MatchString(user) {
		return nil, fmt.Errorf("invalid username")
	}
	sid, err := uuid.Parse(user[len("dc-"):])
	if err != nil {
		return nil, fmt.Errorf("invalid session uuid")
	}

	// 2. Fingerprint of the offered key. Build Permissions from THIS key,
	// per the crypto/ssh "last-key-wins" semantic — never cache from an
	// earlier call.
	fp := ssh.FingerprintSHA256(key)

	// 3. Session-member-scoped lookup: any session-member's key matches.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	matched, err := s.queries.LookupSessionMemberKeyByFingerprint(ctx, db.LookupSessionMemberKeyByFingerprintParams{
		SessionID:   sid,
		Fingerprint: fp,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("not authorized")
		}
		slog.Error("ssh auth db error", "session_id", sid, "fp", fp, "error", err)
		return nil, fmt.Errorf("auth lookup failed")
	}

	// 4. Container reachability pre-check. Without this, auth would
	// succeed but the first channel-open would fail mid-protocol with a
	// confusing error. Refuse here so VS Code sees a clean auth denial.
	session, err := s.queries.GetSession(ctx, sid)
	if err != nil {
		slog.Warn("ssh auth: session lookup failed", "session_id", sid, "error", err)
		return nil, fmt.Errorf("session unavailable")
	}
	if s.workspaces != nil {
		if _, err := s.workspaces.ContainerName(ctx, session.Name); err != nil {
			if errors.Is(err, workspace.ErrContainerNotRunning) {
				return nil, fmt.Errorf("session container not running")
			}
			slog.Warn("ssh auth: container resolve failed", "session_id", sid, "error", err)
			return nil, fmt.Errorf("session unavailable")
		}
	}

	// 5. Async last-used touch — fire and forget; failure here must not
	// block auth. Uses a fresh context so it survives the callback return.
	go func(id uuid.UUID) {
		bg, bgCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer bgCancel()
		if err := s.queries.TouchUserSSHKeyLastUsed(bg, id); err != nil {
			slog.Debug("touch last_used_at failed", "key_id", id, "error", err)
		}
	}(matched.ID)

	return &ssh.Permissions{
		Extensions: map[string]string{
			extSessionID: sid.String(),
			extUserID:    matched.UserID.String(),
			extKeyID:     matched.ID.String(),
			extFP:        fp,
		},
	}, nil
}

// authLogCallback emits a structured log line for every auth attempt
// (pass or fail, any method). Never logs the public key — only the
// fingerprint.
func (s *Server) authLogCallback(meta ssh.ConnMetadata, method string, authErr error) {
	srcIP := remoteIP(meta.RemoteAddr())
	if authErr == nil {
		slog.Info("ssh_auth_ok",
			"user", meta.User(),
			"method", method,
			"src_ip", srcIP,
		)
		return
	}
	// On failure we don't have the fingerprint readily available unless
	// the publicKeyCallback put it on Permissions, which doesn't happen
	// for failed attempts. The user-facing log keeps just method + error
	// class to avoid leaking enumeration signal.
	slog.Info("ssh_auth_fail",
		"user", meta.User(),
		"method", method,
		"src_ip", srcIP,
	)
}

// remoteIP extracts the source IP for logging. Returns the raw addr
// string on parse failure — better to log something than nothing.
func remoteIP(addr net.Addr) string {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
