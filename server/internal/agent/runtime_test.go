package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/forgeutah/deuce/server/internal/agent/pirun"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// --- fake Store -------------------------------------------------------------

type fakeTask struct {
	id, sessionID, agentID, state, prompt, reply string
	order                                        int
}

type fakeStore struct {
	mu    sync.Mutex
	seq   map[string]int64
	tasks map[string]*fakeTask
	order int
	idn   int
}

func newFakeStore() *fakeStore {
	return &fakeStore{seq: map[string]int64{}, tasks: map[string]*fakeTask{}}
}

func (s *fakeStore) nextSeq(sessionID string) int64 {
	s.seq[sessionID]++
	return s.seq[sessionID]
}

func (s *fakeStore) CreateQueuedTask(_ context.Context, p EnqueueParams) (string, int64, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idn++
	id := fmt.Sprintf("task-%d", s.idn)
	s.order++
	s.tasks[id] = &fakeTask{id: id, sessionID: p.SessionID, agentID: p.AgentID, state: StateQueued, prompt: p.Prompt, order: s.order}
	pos := 0
	for _, t := range s.tasks {
		if t.sessionID == p.SessionID && t.agentID == p.AgentID && t.state == StateQueued {
			pos++
		}
	}
	return id, s.nextSeq(p.SessionID), pos, nil
}

func (s *fakeStore) setState(sessionID, taskID, state string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tasks[taskID]; t != nil {
		t.state = state
	}
	return s.nextSeq(sessionID)
}

func (s *fakeStore) MarkRunning(_ context.Context, sessionID, taskID string) (int64, error) {
	return s.setState(sessionID, taskID, StateRunning), nil
}
func (s *fakeStore) SetAwaitingInput(_ context.Context, sessionID, taskID, _ string) (int64, error) {
	return s.setState(sessionID, taskID, StateAwaitingInput), nil
}
func (s *fakeStore) ResolveAwaitingInput(_ context.Context, sessionID, taskID string) (int64, error) {
	return s.setState(sessionID, taskID, StateRunning), nil
}
func (s *fakeStore) FinishTask(_ context.Context, sessionID, taskID, state, reply string) (int64, error) {
	s.mu.Lock()
	if t := s.tasks[taskID]; t != nil {
		t.state = state
		t.reply = reply
	}
	seq := s.nextSeq(sessionID)
	s.mu.Unlock()
	return seq, nil
}
func (s *fakeStore) AppendActionStarted(_ context.Context, sessionID, _, _, _, _ string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSeq(sessionID), nil
}
func (s *fakeStore) CompleteAction(_ context.Context, sessionID, _, _, _ string, _ bool) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nextSeq(sessionID), nil
}

func (s *fakeStore) RunningTask(_ context.Context, sessionID, agentID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := s.firstByOrder(sessionID, agentID, func(st string) bool { return st == StateRunning || st == StateAwaitingInput })
	return best, best != "", nil
}
func (s *fakeStore) NextQueuedTask(_ context.Context, sessionID, agentID string) (string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.firstByOrder(sessionID, agentID, func(st string) bool { return st == StateQueued })
	if id == "" {
		return "", "", false, nil
	}
	return id, s.tasks[id].prompt, true, nil
}
func (s *fakeStore) TaskState(_ context.Context, taskID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tasks[taskID]; t != nil {
		return t.state, true, nil
	}
	return "", false, nil
}

// firstByOrder returns the lowest-order task for the key matching pred. Caller
// holds s.mu.
func (s *fakeStore) firstByOrder(sessionID, agentID string, pred func(string) bool) string {
	best := ""
	bestOrder := 1 << 30
	for _, t := range s.tasks {
		if t.sessionID == sessionID && t.agentID == agentID && pred(t.state) && t.order < bestOrder {
			best, bestOrder = t.id, t.order
		}
	}
	return best
}

func (s *fakeStore) state(taskID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tasks[taskID]; t != nil {
		return t.state
	}
	return ""
}

// --- fake Broadcaster -------------------------------------------------------

type recordedEvent struct {
	typ string
	raw map[string]any
}

type fakeBroadcaster struct {
	mu   sync.Mutex
	evts []recordedEvent
}

func (b *fakeBroadcaster) BroadcastToSession(_ string, msg ws.ServerMessage, _ *ws.Client) {
	var raw map[string]any
	_ = json.Unmarshal(msg.Payload, &raw)
	b.mu.Lock()
	b.evts = append(b.evts, recordedEvent{typ: msg.Type, raw: raw})
	b.mu.Unlock()
}

func (b *fakeBroadcaster) types() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.evts))
	for i, e := range b.evts {
		out[i] = e.typ
	}
	return out
}

func (b *fakeBroadcaster) count(typ string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, e := range b.evts {
		if e.typ == typ {
			n++
		}
	}
	return n
}

func (b *fakeBroadcaster) waitFor(t *testing.T, typ string, n int) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		if b.count(typ) >= n {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d %q events; got types %v", n, typ, b.types())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// --- test Pi launcher (drives real pirun.Supervisor) ------------------------

type tHandle struct {
	inR, outR *io.PipeReader
	inW, outW *io.PipeWriter
	stopOnce  sync.Once
	cmds      chan map[string]any
}

func newTHandle() *tHandle {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &tHandle{inR: inR, inW: inW, outR: outR, outW: outW, cmds: make(chan map[string]any, 32)}
	go func() {
		sc := bufio.NewScanner(inR)
		for sc.Scan() {
			var m map[string]any
			if json.Unmarshal(sc.Bytes(), &m) != nil {
				continue
			}
			if m["type"] == "get_state" {
				_, _ = outW.Write([]byte(`{"type":"response","command":"get_state","success":true}` + "\n"))
			}
			select {
			case h.cmds <- m:
			default:
			}
		}
	}()
	return h
}

func (h *tHandle) waitCmd(t *testing.T, typ string) map[string]any {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case m := <-h.cmds:
			if m["type"] == typ {
				return m
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q command", typ)
		}
	}
}

func (h *tHandle) Stdin() io.WriteCloser { return h.inW }
func (h *tHandle) Stdout() io.Reader     { return h.outR }
func (h *tHandle) Wait() error           { return nil }
func (h *tHandle) Stop(context.Context) error {
	h.stopOnce.Do(func() { _ = h.outW.Close(); _ = h.inW.Close() })
	return nil
}
func (h *tHandle) push(line string) { _, _ = h.outW.Write([]byte(line + "\n")) }

type tLauncher struct {
	mu sync.Mutex
	hs []*tHandle
}

func (l *tLauncher) Launch(context.Context, string, []string) (pirun.Handle, error) {
	h := newTHandle()
	l.mu.Lock()
	l.hs = append(l.hs, h)
	l.mu.Unlock()
	return h, nil
}

func (l *tLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.hs)
}

func (l *tLauncher) handle(t *testing.T, i int) *tHandle {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		l.mu.Lock()
		if len(l.hs) > i {
			h := l.hs[i]
			l.mu.Unlock()
			return h
		}
		l.mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("handle %d never launched", i)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func newTestRuntime(t *testing.T) (*Runtime, *fakeStore, *fakeBroadcaster, *tLauncher) {
	t.Helper()
	store := newFakeStore()
	bc := &fakeBroadcaster{}
	lr := &tLauncher{}
	sup := pirun.NewSupervisor(lr, "test-key")
	rt := NewRuntime(store, sup, bc)
	rt.Start()
	t.Cleanup(func() {
		rt.Shutdown()
		_ = sup.Shutdown(context.Background())
	})
	return rt, store, bc, lr
}

// --- tests ------------------------------------------------------------------

func TestEnqueueRunsAndCompletes(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()

	taskID, err := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "do it", WorkspaceID: "ws"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	bc.waitFor(t, ws.TypeTaskStarted, 1)

	h := lr.handle(t, 0)
	h.push(`{"type":"tool_execution_start","toolCallId":"c1","toolName":"bash","args":{"command":"ls"}}`)
	bc.waitFor(t, ws.TypeActionStarted, 1)
	h.push(`{"type":"tool_execution_end","toolCallId":"c1","toolName":"bash","result":{"content":[{"type":"text","text":"out"}]},"isError":false}`)
	bc.waitFor(t, ws.TypeActionCompleted, 1)
	h.push(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"all done"}}`)
	h.push(`{"type":"agent_end"}`)
	bc.waitFor(t, ws.TypeTaskCompleted, 1)

	if got := store.state(taskID); got != StateDone {
		t.Errorf("task state = %q, want done", got)
	}
	if store.tasks[taskID].reply != "all done" {
		t.Errorf("reply = %q, want 'all done'", store.tasks[taskID].reply)
	}
	// Event order: enqueued → started → action_started → action_completed → completed.
	want := []string{ws.TypeTaskEnqueued, ws.TypeTaskStarted, ws.TypeActionStarted, ws.TypeActionCompleted, ws.TypeTaskCompleted}
	if got := bc.types(); !sameOrder(got, want) {
		t.Errorf("event order = %v, want %v", got, want)
	}
}

func TestSecondMentionQueuesThenPromotes(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()

	t1, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "first", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	t2, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "second", WorkspaceID: "ws"})

	// t2 must stay queued while t1 runs (one running per key).
	if store.state(t2) != StateQueued {
		t.Fatalf("t2 state = %q, want queued", store.state(t2))
	}
	// Finish t1 → t2 promotes.
	lr.handle(t, 0).push(`{"type":"agent_end"}`)
	bc.waitFor(t, ws.TypeTaskStarted, 2)

	if store.state(t1) != StateDone {
		t.Errorf("t1 state = %q, want done", store.state(t1))
	}
	if store.state(t2) != StateRunning {
		t.Errorf("t2 state = %q, want running after promotion", store.state(t2))
	}
}

func TestConcurrentAgentsRunInParallel(t *testing.T) {
	rt, store, bc, _ := newTestRuntime(t)
	ctx := context.Background()
	ca, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "coder", Prompt: "x", WorkspaceID: "ws"})
	ra, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "reviewer", Prompt: "y", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 2)
	if store.state(ca) != StateRunning || store.state(ra) != StateRunning {
		t.Errorf("both agents should run concurrently: coder=%q reviewer=%q", store.state(ca), store.state(ra))
	}
}

func TestProcessExitFailsRunningTask(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "x", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)

	_ = lr.handle(t, 0).Stop(ctx) // process dies
	bc.waitFor(t, ws.TypeTaskCompleted, 1)
	if store.state(task) != StateFailed {
		t.Errorf("task state = %q, want failed", store.state(task))
	}
}

func TestTerminalIsIdempotent(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "x", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)

	h.push(`{"type":"agent_end"}`) // terminal done
	bc.waitFor(t, ws.TypeTaskCompleted, 1)
	_ = h.Stop(ctx) // then process exits — must NOT double-complete

	// Give the exit signal time to be (not) processed.
	time.Sleep(150 * time.Millisecond)
	if n := bc.count(ws.TypeTaskCompleted); n != 1 {
		t.Errorf("task_completed broadcast %d times, want exactly 1 (idempotent terminal)", n)
	}
	if store.state(task) != StateDone {
		t.Errorf("task state = %q, want done (not overwritten by exit)", store.state(task))
	}
}

func TestCancelPromotesNextOntoFreshProcess(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	t1, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "first", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	t2, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "second", WorkspaceID: "ws"})
	if store.state(t2) != StateQueued {
		t.Fatalf("t2 state = %q, want queued", store.state(t2))
	}

	// /stop the agent: t1 is cancelled, its process torn down, and t2 promoted
	// onto a FRESH process (not the one being killed — the bug this guards).
	rt.Cancel(ctx, pirun.Key{SessionID: "s1", AgentID: "a1"})
	bc.waitFor(t, ws.TypeTaskStarted, 2)

	if store.state(t1) != StateCancelled {
		t.Errorf("t1 state = %q, want cancelled", store.state(t1))
	}
	if store.state(t2) != StateRunning {
		t.Errorf("t2 state = %q, want running after cancel-promote", store.state(t2))
	}
	if lr.count() < 2 {
		t.Errorf("expected a fresh process launched for t2, launches = %d", lr.count())
	}
}

func TestRouteFeedsRunningRun(t *testing.T) {
	rt, _, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)

	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "use staging"})
	if err != nil || res != RouteFed {
		t.Fatalf("RouteOrEnqueue = (%v,%v), want (RouteFed,nil)", res, err)
	}
	m := lr.handle(t, 0).waitCmd(t, "steer")
	if m["message"] != "use staging" {
		t.Errorf("steer message = %v, want 'use staging'", m["message"])
	}
}

func TestRouteAnswersAwaitingInput(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)

	h.push(`{"type":"extension_ui_request","id":"ui-1","method":"input","params":{"prompt":"which env?"}}`)
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)
	if store.state(task) != StateAwaitingInput {
		t.Fatalf("state = %q, want awaiting_input", store.state(task))
	}

	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "prod"})
	if err != nil || res != RouteFed {
		t.Fatalf("RouteOrEnqueue = (%v,%v), want RouteFed", res, err)
	}
	m := h.waitCmd(t, "extension_ui_response")
	if m["id"] != "ui-1" || m["response"] != "prod" {
		t.Errorf("extension_ui_response = %v, want id=ui-1 response=prod", m)
	}
	if store.state(task) != StateRunning {
		t.Errorf("state after answer = %q, want running", store.state(task))
	}
}

func TestRouteEnqueuesWhenIdle(t *testing.T) {
	rt, store, bc, _ := newTestRuntime(t)
	ctx := context.Background()
	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "brand new", WorkspaceID: "ws"})
	if err != nil || res != RouteEnqueued {
		t.Fatalf("RouteOrEnqueue idle = (%v,%v), want RouteEnqueued", res, err)
	}
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	// Exactly one task exists and it is running (promoted because idle).
	running, ok, _ := store.RunningTask(ctx, "s1", "a1")
	if !ok || store.state(running) != StateRunning {
		t.Errorf("expected a running task after idle enqueue")
	}
}

func TestAwaitingCeilingFailsTask(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	rt.awaitTimeout = 60 * time.Millisecond // ceiling for the test (same package)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", AgentID: "a1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)

	lr.handle(t, 0).push(`{"type":"extension_ui_request","id":"ui-1","params":{"prompt":"?"}}`)
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)
	// No answer → ceiling fails the task and frees the lane.
	bc.waitFor(t, ws.TypeTaskCompleted, 1)
	if store.state(task) != StateFailed {
		t.Errorf("state = %q, want failed after awaiting ceiling", store.state(task))
	}
}

func sameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
