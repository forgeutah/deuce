package reconcile

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
	"github.com/forgeutah/deuce/server/internal/workspace"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// --- fakes -----------------------------------------------------------------

type fakeQueries struct {
	mu             sync.Mutex
	sessions       []db.Session
	updateCalls    []db.UpdateSessionWorkspaceStatusIfMatchesParams
	updateReturns  map[uuid.UUID]int64 // per-session rows-affected; default 1
	updateErr      error
	listSessionErr error
}

func (f *fakeQueries) ListNonArchivedSessions(ctx context.Context) ([]db.Session, error) {
	if f.listSessionErr != nil {
		return nil, f.listSessionErr
	}
	return f.sessions, nil
}

func (f *fakeQueries) UpdateSessionWorkspaceStatusIfMatches(ctx context.Context, arg db.UpdateSessionWorkspaceStatusIfMatchesParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls = append(f.updateCalls, arg)
	if f.updateErr != nil {
		return 0, f.updateErr
	}
	if f.updateReturns != nil {
		if n, ok := f.updateReturns[arg.ID]; ok {
			return n, nil
		}
	}
	return 1, nil
}

type fakeLister struct {
	containers map[string]workspace.ContainerState
	err        error
}

func (f fakeLister) BulkContainerStatus(ctx context.Context) (map[string]workspace.ContainerState, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.containers, nil
}

type fakeUIDs struct {
	uids   map[string]string // workspaceID → uid (presence implies exists=true)
	errors map[string]error
}

func (f fakeUIDs) WorkspaceUID(workspaceID string) (string, bool, error) {
	if err, ok := f.errors[workspaceID]; ok {
		return "", false, err
	}
	if uid, ok := f.uids[workspaceID]; ok {
		return uid, true, nil
	}
	return "", false, nil
}

type fakeHub struct {
	mu    sync.Mutex
	calls []string // session IDs broadcast to
}

func (f *fakeHub) BroadcastToSession(sessionID string, msg ws.ServerMessage, _ *ws.Client) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sessionID)
}

// newReconcilerForTest builds a Reconciler wired to fakes. New() takes
// concrete types, so this test constructor reaches into the struct directly.
func newReconcilerForTest(q queriesAPI, hub broadcaster, lister ContainerLister, uids WorkspaceUIDReader) *Reconciler {
	return &Reconciler{
		queries:  q,
		hub:      hub,
		lister:   lister,
		uids:     uids,
		interval: 10 * time.Millisecond,
	}
}

func mkSession(name, status string) db.Session {
	return db.Session{
		ID:              uuid.New(),
		Name:            name,
		WorkspaceStatus: status,
		Status:          "active",
	}
}

// --- deriveTruth -----------------------------------------------------------

func TestDeriveTruth_RunningContainerYieldsReady(t *testing.T) {
	r := newReconcilerForTest(nil, nil, nil, fakeUIDs{uids: map[string]string{"ws1": "uid-1"}})
	got, skip := r.deriveTruth("ws1", map[string]workspace.ContainerState{
		"uid-1": workspace.ContainerRunning,
	})
	if skip {
		t.Fatal("unexpected skip")
	}
	if got != "ready" {
		t.Errorf("got %q, want ready", got)
	}
}

func TestDeriveTruth_StoppedContainerYieldsStopped(t *testing.T) {
	r := newReconcilerForTest(nil, nil, nil, fakeUIDs{uids: map[string]string{"ws1": "uid-1"}})
	got, _ := r.deriveTruth("ws1", map[string]workspace.ContainerState{
		"uid-1": workspace.ContainerStopped,
	})
	if got != "stopped" {
		t.Errorf("got %q, want stopped", got)
	}
}

func TestDeriveTruth_NoContainerButMetaYieldsStopped(t *testing.T) {
	r := newReconcilerForTest(nil, nil, nil, fakeUIDs{uids: map[string]string{"ws1": "uid-1"}})
	// uid is registered (devpod knows about the workspace) but no docker
	// container with that label is present — devpod considers it stopped.
	got, _ := r.deriveTruth("ws1", map[string]workspace.ContainerState{})
	if got != "stopped" {
		t.Errorf("got %q, want stopped", got)
	}
}

func TestDeriveTruth_NoContainerNoMetaYieldsMissing(t *testing.T) {
	r := newReconcilerForTest(nil, nil, nil, fakeUIDs{})
	got, _ := r.deriveTruth("ws-orphan", map[string]workspace.ContainerState{})
	if got != "missing" {
		t.Errorf("got %q, want missing", got)
	}
}

func TestDeriveTruth_UIDReadErrorSkips(t *testing.T) {
	r := newReconcilerForTest(nil, nil, nil, fakeUIDs{errors: map[string]error{"ws1": errors.New("perm denied")}})
	_, skip := r.deriveTruth("ws1", map[string]workspace.ContainerState{})
	if !skip {
		t.Fatal("expected skip on uid read error")
	}
}

// --- tick ------------------------------------------------------------------

func TestTick_NoDriftIssuesNoWrites(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws1", "ready")
	q := &fakeQueries{sessions: []db.Session{s}}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		fakeLister{containers: map[string]workspace.ContainerState{"uid-1": workspace.ContainerRunning}},
		fakeUIDs{uids: map[string]string{"ws1": "uid-1"}},
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 0 {
		t.Errorf("expected 0 updates, got %d", len(q.updateCalls))
	}
	if len(hub.calls) != 0 {
		t.Errorf("expected 0 broadcasts, got %d", len(hub.calls))
	}
}

func TestTick_DriftReadyToStoppedWrites(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws1", "ready")
	q := &fakeQueries{sessions: []db.Session{s}}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		// Container exited — should flip to stopped.
		fakeLister{containers: map[string]workspace.ContainerState{"uid-1": workspace.ContainerStopped}},
		fakeUIDs{uids: map[string]string{"ws1": "uid-1"}},
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 1 {
		t.Fatalf("expected 1 update, got %d", len(q.updateCalls))
	}
	call := q.updateCalls[0]
	if call.NewStatus != "stopped" {
		t.Errorf("NewStatus = %q, want stopped", call.NewStatus)
	}
	if call.ExpectedStatus != "ready" {
		t.Errorf("ExpectedStatus = %q, want ready (CAS guard)", call.ExpectedStatus)
	}
	if len(hub.calls) != 1 || hub.calls[0] != s.ID.String() {
		t.Errorf("expected one broadcast for %s, got %v", s.ID, hub.calls)
	}
}

func TestTick_DriftStoppedToReadyWrites(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws1", "stopped")
	q := &fakeQueries{sessions: []db.Session{s}}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		fakeLister{containers: map[string]workspace.ContainerState{"uid-1": workspace.ContainerRunning}},
		fakeUIDs{uids: map[string]string{"ws1": "uid-1"}},
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 1 || q.updateCalls[0].NewStatus != "ready" {
		t.Errorf("expected one update to ready, got %+v", q.updateCalls)
	}
}

func TestTick_MissingWithMetaYieldsStopped(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws1", "ready")
	q := &fakeQueries{sessions: []db.Session{s}}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		fakeLister{containers: map[string]workspace.ContainerState{}}, // no container
		fakeUIDs{uids: map[string]string{"ws1": "uid-1"}},
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 1 || q.updateCalls[0].NewStatus != "stopped" {
		t.Errorf("expected update to stopped, got %+v", q.updateCalls)
	}
}

func TestTick_MissingWithoutMetaYieldsMissing(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws-gone", "ready")
	q := &fakeQueries{sessions: []db.Session{s}}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		fakeLister{containers: map[string]workspace.ContainerState{}},
		fakeUIDs{}, // empty — no on-disk metadata
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 1 || q.updateCalls[0].NewStatus != "missing" {
		t.Errorf("expected update to missing, got %+v", q.updateCalls)
	}
}

func TestTick_TransitionalSessionsSkipped(t *testing.T) {
	ctx := context.Background()
	sessions := []db.Session{
		mkSession("ws-a", "starting"),
		mkSession("ws-b", "stopping"),
		mkSession("ws-c", "rebuilding"),
		mkSession("ws-d", "deleting"),
	}
	q := &fakeQueries{sessions: sessions}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		// All containers absent — would normally write 'missing' or 'stopped'
		// for terminal-state rows. Transitional rows must be skipped instead.
		fakeLister{containers: map[string]workspace.ContainerState{}},
		fakeUIDs{},
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 0 {
		t.Errorf("expected zero writes for transitional rows, got %d", len(q.updateCalls))
	}
}

func TestTick_DockerErrorAbortsWithoutWrites(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws1", "ready")
	q := &fakeQueries{sessions: []db.Session{s}}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		fakeLister{err: errors.New("docker daemon down")},
		fakeUIDs{uids: map[string]string{"ws1": "uid-1"}},
	)
	err := r.tick(ctx)
	if err == nil {
		t.Fatal("expected error from tick")
	}
	if len(q.updateCalls) != 0 {
		t.Errorf("expected no writes on docker error, got %d", len(q.updateCalls))
	}
	if len(hub.calls) != 0 {
		t.Errorf("expected no broadcasts on docker error, got %d", len(hub.calls))
	}
}

func TestTick_CASZeroRowsSuppressesBroadcast(t *testing.T) {
	ctx := context.Background()
	s := mkSession("ws1", "ready")
	q := &fakeQueries{
		sessions:      []db.Session{s},
		updateReturns: map[uuid.UUID]int64{s.ID: 0}, // CAS lost — row moved since SELECT
	}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub,
		fakeLister{containers: map[string]workspace.ContainerState{"uid-1": workspace.ContainerStopped}},
		fakeUIDs{uids: map[string]string{"ws1": "uid-1"}},
	)
	if err := r.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if len(q.updateCalls) != 1 {
		t.Fatalf("expected 1 attempted update, got %d", len(q.updateCalls))
	}
	if len(hub.calls) != 0 {
		t.Errorf("expected no broadcast when CAS reported 0 rows, got %d", len(hub.calls))
	}
}

// --- lifecycle -------------------------------------------------------------

func TestNew_DefaultsIntervalWhenZero(t *testing.T) {
	r := New(nil, nil, nil, nil, 0)
	if r.interval != 10*time.Second {
		t.Errorf("interval = %s, want 10s", r.interval)
	}
}

func TestRun_ExitsOnContextCancel(t *testing.T) {
	q := &fakeQueries{}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub, fakeLister{}, fakeUIDs{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()
	cancel()

	select {
	case <-done:
		// good
	case <-time.After(time.Second):
		t.Fatal("Run did not exit within 1s of ctx cancel")
	}
}

func TestShutdown_WaitsForInFlightTick(t *testing.T) {
	q := &fakeQueries{}
	hub := &fakeHub{}
	r := newReconcilerForTest(q, hub, fakeLister{}, fakeUIDs{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	// Give Run a moment to enter its loop and add to the WaitGroup.
	time.Sleep(20 * time.Millisecond)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutCancel()
	cancel() // trigger Run exit
	if err := r.Shutdown(shutCtx); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
