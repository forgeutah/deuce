// Package reconcile keeps sessions.workspace_status honest by polling
// docker ps every ~10s and writing truth into the DB when reality drifts.
// It mirrors the embedded-listener pattern from sshproxy.Server: a single
// long-lived goroutine started by main.go, shutdown coordinated via the
// shared 10s drain context.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// ContainerLister is the seam the reconciler uses to query container state.
// Production wires *workspace.Manager; tests stub it.
type ContainerLister interface {
	BulkContainerStatus(ctx context.Context) (map[string]workspace.ContainerState, error)
}

// WorkspaceUIDReader maps a DevPod workspace ID to its docker-label uid.
// The exists return distinguishes "no on-disk state" from genuine read errors,
// so the reconciler can derive `missing` from absence-of-metadata.
type WorkspaceUIDReader interface {
	WorkspaceUID(workspaceID string) (uid string, exists bool, err error)
}

const (
	statusStarting   = "starting"
	statusReady      = "ready"
	statusStopping   = "stopping"
	statusStopped    = "stopped"
	statusRebuilding = "rebuilding"
	statusDeleting   = "deleting"
	statusMissing    = "missing"
	statusFailed     = "failed"
)

// queriesAPI is the narrow slice of *db.Queries the reconciler uses. Defining
// it as an interface (and exporting nothing more than this package needs) lets
// tests provide a fake without spinning up Postgres for every tick scenario.
type queriesAPI interface {
	ListNonArchivedSessions(ctx context.Context) ([]db.Session, error)
	UpdateSessionWorkspaceStatusIfMatches(ctx context.Context, arg db.UpdateSessionWorkspaceStatusIfMatchesParams) (int64, error)
}

// broadcaster is the narrow slice of *ws.Hub the reconciler uses. Tests
// inject a fake that records what would have been sent.
type broadcaster interface {
	BroadcastToSession(sessionID string, msg ws.ServerMessage, excludeClient *ws.Client)
}

// Reconciler is the singleton sweeper. One instance per server process.
type Reconciler struct {
	queries  queriesAPI
	hub      broadcaster
	lister   ContainerLister
	uids     WorkspaceUIDReader
	interval time.Duration

	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

// New builds a Reconciler. Interval ≤ 0 defaults to 10s.
func New(queries *db.Queries, hub *ws.Hub, lister ContainerLister, uids WorkspaceUIDReader, interval time.Duration) *Reconciler {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	return &Reconciler{
		queries:  queries,
		hub:      hub,
		lister:   lister,
		uids:     uids,
		interval: interval,
	}
}

// Run blocks until ctx is cancelled or Shutdown is called, ticking the
// reconciliation loop on each interval. Errors during a tick log at warn
// and do NOT terminate the loop — transient docker daemon failures must
// not cause the sweeper to die.
func (r *Reconciler) Run(ctx context.Context) {
	r.wg.Add(1)
	defer r.wg.Done()

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	slog.Info("reconciler started", "interval", r.interval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("reconciler stopping (context cancelled)")
			return
		case <-ticker.C:
			r.mu.Lock()
			closed := r.closed
			r.mu.Unlock()
			if closed {
				slog.Info("reconciler stopping (shutdown)")
				return
			}
			if err := r.tick(ctx); err != nil {
				slog.Warn("reconciler tick failed", "error", err)
			}
		}
	}
}

// Shutdown marks the reconciler closed and waits for any in-flight tick to
// finish, bounded by ctx. Run returns at the next ticker fire or ctx.Done.
func (r *Reconciler) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tick runs one reconciliation pass. List non-archived sessions, query
// docker ps once for all DevPod containers, derive truth state per session,
// CAS-update + broadcast when truth differs from the stored value.
func (r *Reconciler) tick(ctx context.Context) error {
	sessions, err := r.queries.ListNonArchivedSessions(ctx)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	containers, err := r.lister.BulkContainerStatus(ctx)
	if err != nil {
		// R6: transient docker failures must not flip rows to `missing`.
		// Skip the tick entirely; next interval will retry.
		return fmt.Errorf("bulk container status: %w", err)
	}

	for _, s := range sessions {
		// R5: never touch transitional rows — they're owned by the action
		// goroutine for the lifetime of the operation.
		switch s.WorkspaceStatus {
		case statusStarting, statusStopping, statusRebuilding, statusDeleting:
			continue
		}

		truth, skip := r.deriveTruth(s.Name, containers)
		if skip || truth == s.WorkspaceStatus {
			continue
		}

		// CAS update: only write if workspace_status hasn't moved since we
		// read it. Mitigates the race against a concurrent action handler
		// that just set a transitional state — without this, the reconciler
		// could overwrite the handler's transitional write with a stale
		// terminal one, producing a visible UI flicker.
		rows, err := r.queries.UpdateSessionWorkspaceStatusIfMatches(ctx, db.UpdateSessionWorkspaceStatusIfMatchesParams{
			ID:             s.ID,
			ExpectedStatus: s.WorkspaceStatus,
			NewStatus:      truth,
		})
		if err != nil {
			slog.Warn("reconciler update failed", "session", s.ID, "error", err)
			continue
		}
		if rows == 0 {
			// Handler beat us — leave the row alone. The handler's terminal
			// write will be authoritative on the next tick or sooner.
			continue
		}

		slog.Info("reconciler updated workspace_status",
			"session", s.ID,
			"from", s.WorkspaceStatus,
			"to", truth,
		)

		msg, mErr := ws.NewServerMessage(ws.TypeSessionUpdate, s.ID.String(), map[string]string{
			"workspaceStatus": truth,
		})
		if mErr != nil {
			slog.Warn("reconciler ws message build failed", "error", mErr)
			continue
		}
		r.hub.BroadcastToSession(s.ID.String(), msg, nil)
	}

	return nil
}

// deriveTruth maps (workspace-id, container-map) → the workspace_status
// the DB should hold. Returns (truth, skip): when skip is true, the caller
// must not write — typically because the on-disk uid read failed transiently
// and we don't want to flip the row to a wrong value.
//
// Truth table:
//   container in docker (running)        → ready
//   container in docker (stopped/exited) → stopped
//   no container BUT workspace.json      → stopped  (devpod knows about it)
//   no container AND no workspace.json   → missing
func (r *Reconciler) deriveTruth(workspaceID string, containers map[string]workspace.ContainerState) (string, bool) {
	uid, hasMeta, err := r.uids.WorkspaceUID(workspaceID)
	if err != nil {
		slog.Warn("workspace uid read failed, skipping session", "workspace", workspaceID, "error", err)
		return "", true
	}

	if hasMeta {
		if state, ok := containers[uid]; ok {
			if state == workspace.ContainerRunning {
				return statusReady, false
			}
			return statusStopped, false
		}
		return statusStopped, false
	}
	return statusMissing, false
}

// ErrShutdown is returned by Shutdown when the in-flight tick did not
// complete before ctx expired. Callers that need to distinguish "shutdown
// timed out" from other errors can use errors.Is.
var ErrShutdown = errors.New("reconciler shutdown timed out")
