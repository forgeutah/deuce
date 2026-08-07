package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forgeutah/deuce/server/internal/agent/pirun"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// --- fake Store -------------------------------------------------------------

type fakeTask struct {
	id, sessionID, state, prompt, reply string
	order                               int
}

type fakeStore struct {
	mu           sync.Mutex
	seq          map[string]int64
	tasks        map[string]*fakeTask
	order        int
	idn          int
	systemPrompt string
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
	s.tasks[id] = &fakeTask{id: id, sessionID: p.SessionID, state: StateQueued, prompt: p.Prompt, order: s.order}
	pos := 0
	for _, t := range s.tasks {
		if t.sessionID == p.SessionID && t.state == StateQueued {
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
func (s *fakeStore) SetAwaitingInput(_ context.Context, sessionID, taskID, _, _ string, _ []string) (int64, error) {
	return s.setState(sessionID, taskID, StateAwaitingInput), nil
}
func (s *fakeStore) DeuceSystemPrompt(_ context.Context) (string, error) {
	return s.systemPrompt, nil
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

func (s *fakeStore) RunningTask(_ context.Context, sessionID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	best := s.firstByOrder(sessionID, func(st string) bool { return st == StateRunning || st == StateAwaitingInput })
	return best, best != "", nil
}
func (s *fakeStore) NextQueuedTask(_ context.Context, sessionID string) (string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.firstByOrder(sessionID, func(st string) bool { return st == StateQueued })
	if id == "" {
		return "", "", false, nil
	}
	return id, s.tasks[id].prompt, true, nil
}
func (s *fakeStore) QueuedTaskIDs(_ context.Context, sessionID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var queued []*fakeTask
	for _, t := range s.tasks {
		if t.sessionID == sessionID && t.state == StateQueued {
			queued = append(queued, t)
		}
	}
	sort.Slice(queued, func(i, j int) bool { return queued[i].order < queued[j].order })
	ids := make([]string, 0, len(queued))
	for _, t := range queued {
		ids = append(ids, t.id)
	}
	return ids, nil
}
func (s *fakeStore) TaskState(_ context.Context, taskID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tasks[taskID]; t != nil {
		return t.state, true, nil
	}
	return "", false, nil
}

// firstByOrder returns the session's lowest-order task matching pred. Caller
// holds s.mu.
func (s *fakeStore) firstByOrder(sessionID string, pred func(string) bool) string {
	best := ""
	bestOrder := 1 << 30
	for _, t := range s.tasks {
		if t.sessionID == sessionID && pred(t.state) && t.order < bestOrder {
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

func (l *tLauncher) Launch(context.Context, string, []string, string) (pirun.Handle, error) {
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
	rt := NewRuntime(store, sup, bc, "")
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

	taskID, err := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "do it", WorkspaceID: "ws"})
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

	t1, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "first", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	t2, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "second", WorkspaceID: "ws"})

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

func TestConcurrentSessionsRunInParallel(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	a, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "x", WorkspaceID: "ws1"})
	b, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s2", Prompt: "y", WorkspaceID: "ws2"})
	bc.waitFor(t, ws.TypeTaskStarted, 2)
	if store.state(a) != StateRunning || store.state(b) != StateRunning {
		t.Errorf("both sessions should run concurrently: s1=%q s2=%q", store.state(a), store.state(b))
	}
	if lr.count() != 2 {
		t.Errorf("expected one Pi process per session, launches = %d", lr.count())
	}
}

func TestProcessExitFailsRunningTask(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "x", WorkspaceID: "ws"})
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
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "x", WorkspaceID: "ws"})
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

func TestReplyPosterReceivesReply(t *testing.T) {
	rt, _, bc, lr := newTestRuntime(t)
	var mu sync.Mutex
	var got string
	rt.SetReplyPoster(func(_, reply string) { mu.Lock(); got = reply; mu.Unlock() })

	ctx := context.Background()
	rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "hi", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)
	h.push(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"hello there"}}`)
	h.push(`{"type":"agent_end"}`)
	bc.waitFor(t, ws.TypeTaskCompleted, 1)

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		g := got
		mu.Unlock()
		if g == "hello there" {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("replyPoster received %q, want 'hello there'", g)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestCancelSessionCancelsRunningAndQueued(t *testing.T) {
	rt, store, bc, _ := newTestRuntime(t)
	ctx := context.Background()
	t1, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "first", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	t2, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "second", WorkspaceID: "ws"})
	if store.state(t2) != StateQueued {
		t.Fatalf("t2 state = %q, want queued", store.state(t2))
	}

	// /stop: the queued task is drained AND the running task cancelled (R6) —
	// nothing is promoted seconds after the user asked for quiet.
	rt.CancelSession(ctx, "s1")
	bc.waitFor(t, ws.TypeTaskCompleted, 2)

	if store.state(t1) != StateCancelled {
		t.Errorf("t1 state = %q, want cancelled", store.state(t1))
	}
	if store.state(t2) != StateCancelled {
		t.Errorf("t2 state = %q, want cancelled (queue drained by /stop)", store.state(t2))
	}
	// Give any stray promotion a moment, then confirm nothing restarted.
	time.Sleep(150 * time.Millisecond)
	if n := bc.count(ws.TypeTaskStarted); n != 1 {
		t.Errorf("task_started broadcast %d times, want exactly 1 (no post-cancel promotion)", n)
	}
}

func TestRecycleIdleStopsOnlyIdleSessions(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()

	// s1 finishes a task cleanly — its process stays alive (reuse) but idle.
	rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "a", WorkspaceID: "ws1"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	lr.handle(t, 0).push(`{"type":"agent_end"}`)
	bc.waitFor(t, ws.TypeTaskCompleted, 1)

	// s2 is mid-task (running).
	t2, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s2", Prompt: "b", WorkspaceID: "ws2"})
	bc.waitFor(t, ws.TypeTaskStarted, 2)

	// s3 is blocked on a question (awaiting_input) — busy, must NOT recycle.
	t3, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s3", Prompt: "c", WorkspaceID: "ws3"})
	bc.waitFor(t, ws.TypeTaskStarted, 3)
	lr.handle(t, 2).push(uiRequestLine(t, "input"))
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)

	rt.RecycleIdleProcesses()

	// Busy sessions keep their tasks live.
	time.Sleep(150 * time.Millisecond)
	if store.state(t2) != StateRunning {
		t.Errorf("s2 task state = %q, want running (busy session must not recycle)", store.state(t2))
	}
	if store.state(t3) != StateAwaitingInput {
		t.Errorf("s3 task state = %q, want awaiting_input (awaiting session must not recycle)", store.state(t3))
	}
	// s1's idle process was deregistered synchronously: a new enqueue launches
	// a FRESH process (the recycle race guard), picking up the new prompt.
	launchesBefore := lr.count()
	rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "again", WorkspaceID: "ws1"})
	bc.waitFor(t, ws.TypeTaskStarted, 4)
	if lr.count() != launchesBefore+1 {
		t.Errorf("expected a fresh launch for s1 after recycle, launches = %d (was %d)", lr.count(), launchesBefore)
	}
}

func TestRouteFeedsRunningRun(t *testing.T) {
	rt, _, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)

	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "use staging"})
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
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)

	h.push(uiRequestLine(t, "input"))
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)
	if store.state(task) != StateAwaitingInput {
		t.Fatalf("state = %q, want awaiting_input", store.state(task))
	}

	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "prod"})
	if err != nil || res != RouteFed {
		t.Fatalf("RouteOrEnqueue = (%v,%v), want RouteFed", res, err)
	}
	// The answer must be correlated back to the request id that opened the
	// dialog AND ride in the arm Pi's parser reads for this method — the old
	// `response` field was accepted and discarded, so the id alone proves
	// nothing about the user's answer arriving.
	m := h.waitCmd(t, "extension_ui_response")
	assertResponseMatchesArm(t, m, "value", "ui-input-1", "prod")
	if store.state(task) != StateRunning {
		t.Errorf("state after answer = %q, want running", store.state(task))
	}
}

// --- answer round trips (U3 / KTD3) -----------------------------------------
//
// Each of these drives a real Pi request line from the contract fixture through
// to the response Deuce writes back, and asserts that response against the
// fixture's own arm rather than against a literal written beside the code under
// test. The steer-while-running case (no question pending) is covered by
// TestRouteFeedsRunningRun above.

// TestAnswerSelectSendsChosenLabel covers AE1/R5: Pi's select response is the
// chosen option's LABEL in the `value` arm, not an index and not a `response`.
func TestAnswerSelectSendsChosenLabel(t *testing.T) {
	rt, store, _, h, task := awaitingOnFixture(t, "select")

	res, err := rt.RouteOrEnqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: "Vue"})
	if err != nil || res != RouteFed {
		t.Fatalf("RouteOrEnqueue = (%v,%v), want RouteFed", res, err)
	}
	m := h.waitCmd(t, "extension_ui_response")
	// The fixture's value arm is exactly this answer to exactly this request.
	if want := uiResponseLine(t, "value"); !reflect.DeepEqual(m, want) {
		t.Errorf("extension_ui_response = %v, want %v (the fixture's value arm)", m, want)
	}
	if store.state(task) != StateRunning {
		t.Errorf("state after answer = %q, want running", store.state(task))
	}
}

// TestAnswerSelectWithTypedTextSendsItUnchanged covers R7: the drawer keeps its
// "or type another answer below" path, and typed text answers a pick-one
// question verbatim.
func TestAnswerSelectWithTypedTextSendsItUnchanged(t *testing.T) {
	rt, _, _, h, _ := awaitingOnFixture(t, "select")

	const typed = "None of these — use Solid"
	if _, err := rt.RouteOrEnqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: typed}); err != nil {
		t.Fatalf("RouteOrEnqueue: %v", err)
	}
	assertResponseMatchesArm(t, h.waitCmd(t, "extension_ui_response"), "value", "ui-select-1", typed)
}

// TestAnswerInputSendsTypedText covers R5 for the free-text style.
func TestAnswerInputSendsTypedText(t *testing.T) {
	rt, _, _, h, _ := awaitingOnFixture(t, "input")

	if _, err := rt.RouteOrEnqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: "staging"}); err != nil {
		t.Fatalf("RouteOrEnqueue: %v", err)
	}
	assertResponseMatchesArm(t, h.waitCmd(t, "extension_ui_response"), "value", "ui-input-1", "staging")
}

// TestAnswerConfirmSendsBoolean covers AE3/R6: Yes and No reach Pi as booleans
// in the `confirmed` arm. Sending the drawer's literal "yes"/"no" string in a
// `response` field is what made every yes/no answer read as No.
func TestAnswerConfirmSendsBoolean(t *testing.T) {
	for _, tc := range []struct {
		name, answer string
		want         bool
	}{
		{"yes button", "yes", true},
		{"no button", "no", false},
		// AE8: the composer stays live beside the buttons, so free text is a
		// reachable input on a yes/no question.
		{"affirmative free text", "yes, go ahead", true},
		{"uppercase with punctuation", "Yeah!", true},
		{"negative free text", "no, stop", false},
		// A narrow token set is indistinguishable from a refusal at the wire:
		// these are ordinary ways to approve, and defaulting them to false
		// delivers the user's approval to the agent as a No.
		{"yep", "yep", true},
		{"okay", "Okay.", true},
		{"go ahead", "go ahead", true},
		{"do it", "do it", true},
		{"proceed", "Proceed", true},
		{"approved", "approved", true},
		{"nah", "nah", false},
		{"cancel", "cancel that", false},
		// "don't" reaches leadingWord as "don" — the apostrophe ends the run.
		{"contraction negative", "don't", false},
		// Near-miss: still not recognized, so it must still default negative.
		// An unparsed reply must never read as approval (R6).
		{"near miss stays negative", "yesterday's build was fine", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, _, h, _ := awaitingOnFixture(t, "confirm")

			if _, err := rt.RouteOrEnqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: tc.answer}); err != nil {
				t.Fatalf("RouteOrEnqueue: %v", err)
			}
			m := h.waitCmd(t, "extension_ui_response")
			assertResponseMatchesArm(t, m, "confirmed", "ui-confirm-1", tc.want)
			if tc.want {
				// Guard the exact bug: a Yes that arrives as anything but a true
				// boolean is discarded by Pi's parser and falls back to false.
				if b, ok := m["confirmed"].(bool); !ok || !b {
					t.Errorf("confirmed = %#v, want the JSON boolean true", m["confirmed"])
				}
			}
		})
	}
}

// TestAnswerConfirmUnrecognizedTextIsNegativeAndLogged: an answer matching
// neither token set defaults to false and is logged at warn. Defaulting negative
// means an unparsed reply can never read as approval, and it matches Pi's own
// confirm fallback.
func TestAnswerConfirmUnrecognizedTextIsNegativeAndLogged(t *testing.T) {
	logs := captureLogs(t)
	rt, _, _, h, _ := awaitingOnFixture(t, "confirm")

	if _, err := rt.RouteOrEnqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: "not sure"}); err != nil {
		t.Fatalf("RouteOrEnqueue: %v", err)
	}
	m := h.waitCmd(t, "extension_ui_response")
	assertResponseMatchesArm(t, m, "confirmed", "ui-confirm-1", false)

	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "unrecognized yes/no answer") {
		t.Errorf("want a WARN log naming the unrecognized answer, got:\n%s", out)
	}
	if !strings.Contains(out, "not sure") {
		t.Errorf("warn log should carry the answer text for diagnosis, got:\n%s", out)
	}
}

// TestAnswerClearsPendingDialog: the pending record is dropped once answered, so
// a later reply on the same task cannot be mistaken for a second answer to a
// dialog Pi has already resolved.
func TestAnswerClearsPendingDialog(t *testing.T) {
	rt, _, _, h, task := awaitingOnFixture(t, "input")
	if _, ok := rt.pendingDialog(task); !ok {
		t.Fatalf("no pending dialog tracked while awaiting_input")
	}

	if _, err := rt.RouteOrEnqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: "staging"}); err != nil {
		t.Fatalf("RouteOrEnqueue: %v", err)
	}
	h.waitCmd(t, "extension_ui_response")

	if pend, ok := rt.pendingDialog(task); ok {
		t.Errorf("pending dialog still tracked after answering: %+v", pend)
	}
}

// TestAwaitingWithoutTrackedDialogSteers: with a task marked awaiting but no
// tracked dialog there is no arm to fill, so the reply falls through to the
// existing steer path rather than emitting an armless response Pi would resolve
// to its own fallback. Boot recovery fails every awaiting_input task before the
// scheduler starts, so this is a tracking-gap guard, not a restart path.
func TestAwaitingWithoutTrackedDialogSteers(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)

	// Awaiting in the store, with nothing tracked in the runtime.
	store.setState("s1", task, StateAwaitingInput)
	if _, ok := rt.pendingDialog(task); ok {
		t.Fatalf("precondition: no dialog should be tracked")
	}

	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "carry on"})
	if err != nil || res != RouteFed {
		t.Fatalf("RouteOrEnqueue = (%v,%v), want RouteFed", res, err)
	}
	if m := h.waitCmd(t, "steer"); m["message"] != "carry on" {
		t.Errorf("steer = %v, want message 'carry on'", m)
	}
}

func TestRouteEnqueuesWhenIdle(t *testing.T) {
	rt, store, bc, _ := newTestRuntime(t)
	ctx := context.Background()
	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "brand new", WorkspaceID: "ws"})
	if err != nil || res != RouteEnqueued {
		t.Fatalf("RouteOrEnqueue idle = (%v,%v), want RouteEnqueued", res, err)
	}
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	// Exactly one task exists and it is running (promoted because idle).
	running, ok, _ := store.RunningTask(ctx, "s1")
	if !ok || store.state(running) != StateRunning {
		t.Errorf("expected a running task after idle enqueue")
	}
}

// TestAwaitCeilingMatchesContractFixture pins the Go half of the KTD7
// cross-language invariant. The extension's PI_DIALOG_TIMEOUT_MS must stay
// strictly above defaultAwaitTimeout so Deuce's ceiling always fires first; if
// Pi's timer won the race it would resolve the dialog with its own default and
// hand the model a fabricated answer. That ordering used to be enforced by two
// hand-written comments and a hardcoded copy of this constant in
// ask-user.test.ts, so a one-sided edit could invert it silently. The contract
// fixture is now the single source of truth: this test binds the Go constant to
// it, and ask-user.test.ts reads the same field for its strictly-greater
// assertion. The fixture lives under pirun/testdata because both sides assert
// against one file; deuceAwaitCeilingMs is Deuce's own value, not Pi's contract.
func TestAwaitCeilingMatchesContractFixture(t *testing.T) {
	var f struct {
		DeuceAwaitCeilingMs int64 `json:"deuceAwaitCeilingMs"`
	}
	if err := json.Unmarshal(readProtocolFixture(t), &f); err != nil {
		t.Fatalf("parse pi-ui-protocol fixture: %v", err)
	}
	if f.DeuceAwaitCeilingMs == 0 {
		t.Fatal("fixture has no deuceAwaitCeilingMs — the KTD7 invariant has no shared anchor")
	}
	if got := defaultAwaitTimeout.Milliseconds(); got != f.DeuceAwaitCeilingMs {
		t.Errorf("defaultAwaitTimeout = %dms, fixture deuceAwaitCeilingMs = %dms — "+
			"move both together or the extension's dialog timeout may no longer sit above Deuce's ceiling (KTD7)",
			got, f.DeuceAwaitCeilingMs)
	}
}

func TestAwaitingCeilingFailsTask(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	rt.awaitTimeout = 60 * time.Millisecond // ceiling for the test (same package)
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)

	lr.handle(t, 0).push(uiRequestLine(t, "input"))
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)
	// No answer → ceiling fails the task and frees the lane.
	bc.waitFor(t, ws.TypeTaskCompleted, 1)
	if store.state(task) != StateFailed {
		t.Errorf("state = %q, want failed after awaiting ceiling", store.state(task))
	}
}

// TestAwaitingSuspendsActiveTimeout covers AE5 / R4: a decoded blocking request
// suspends the active-work budget and starts the awaiting ceiling instead, so a
// question outliving the active budget is still answerable. This is the timeout
// the original bug never reached — the select line was dropped before it could
// fire, so the ten-minute active budget killed the task with the question still
// unanswered.
func TestAwaitingSuspendsActiveTimeout(t *testing.T) {
	rt, store, bc, lr := newTestRuntime(t)
	// Active budget expires almost immediately; the ceiling is generous. If the
	// question did not suspend the active timer, the task would be failed.
	rt.activeTimeout = 200 * time.Millisecond
	rt.awaitTimeout = 30 * time.Second
	ctx := context.Background()
	task, _ := rt.Enqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "go", WorkspaceID: "ws"})
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)

	h.push(uiRequestLine(t, "select"))
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)

	// Well past the active budget, with the ceiling nowhere near.
	time.Sleep(500 * time.Millisecond)
	if got := store.state(task); got != StateAwaitingInput {
		t.Fatalf("state = %q past the active budget, want awaiting_input (the active timeout must be suspended while a question is pending)", got)
	}

	// And it is still answerable.
	res, err := rt.RouteOrEnqueue(ctx, EnqueueParams{SessionID: "s1", Prompt: "Vue"})
	if err != nil || res != RouteFed {
		t.Fatalf("RouteOrEnqueue = (%v,%v), want RouteFed — a question past the active budget must still be answerable", res, err)
	}
	if m := h.waitCmd(t, "extension_ui_response"); m["id"] != "ui-select-1" {
		t.Errorf("extension_ui_response = %v, want id=ui-select-1", m)
	}
	if got := store.state(task); got != StateRunning {
		t.Errorf("state after answer = %q, want running", got)
	}
}

// awaitingOnFixture starts a task and drives it to awaiting_input by pushing the
// named request arm from the contract fixture, returning the runtime, its store,
// the broadcaster, the Pi handle and the blocked task id.
func awaitingOnFixture(t *testing.T, request string) (*Runtime, *fakeStore, *fakeBroadcaster, *tHandle, string) {
	t.Helper()
	rt, store, bc, lr := newTestRuntime(t)
	task, err := rt.Enqueue(context.Background(), EnqueueParams{SessionID: "s1", Prompt: "go", WorkspaceID: "ws"})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	bc.waitFor(t, ws.TypeTaskStarted, 1)
	h := lr.handle(t, 0)
	h.push(uiRequestLine(t, request))
	bc.waitFor(t, ws.TypeTaskAwaitingInput, 1)
	if store.state(task) != StateAwaitingInput {
		t.Fatalf("state = %q, want awaiting_input", store.state(task))
	}
	return rt, store, bc, h, task
}

// assertResponseMatchesArm checks an emitted extension_ui_response against the
// fixture's arm for `arm`, with the id and answer substituted. The comparison is
// over the whole map, so a second arm or a stray legacy `response` field fails
// it — Pi's dispatcher correlates on type+id only and hands the raw object to a
// per-method parser, so an extra key is not an error on the wire and only an
// exact-shape assertion can catch one.
func assertResponseMatchesArm(t *testing.T, got map[string]any, arm, id string, answer any) {
	t.Helper()
	want := uiResponseLine(t, arm)
	want["id"] = id
	want[arm] = answer
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extension_ui_response = %v, want %v (Pi's %q arm)", got, want, arm)
	}
}

// readProtocolFixture returns the parsed contract fixture the decoder tests also
// assert against (server/internal/agent/pirun/testdata/pi-ui-protocol.json).
func readProtocolFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("pirun", "testdata", "pi-ui-protocol.json"))
	if err != nil {
		t.Fatalf("read pi-ui-protocol fixture: %v", err)
	}
	return b
}

// uiResponseLine returns a named arm of Pi's published RpcExtensionUIResponse
// union ("value", "confirmed", "cancelled") as a decoded JSON object, so the
// runtime's answers are asserted against the transcribed contract rather than
// against literals written next to the code that builds them (KTD6).
func uiResponseLine(t *testing.T, arm string) map[string]any {
	t.Helper()
	var f struct {
		Responses map[string]struct {
			Line map[string]any `json:"line"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(readProtocolFixture(t), &f); err != nil {
		t.Fatalf("parse pi-ui-protocol fixture: %v", err)
	}
	r, ok := f.Responses[arm]
	if !ok {
		t.Fatalf("fixture has no response arm %q", arm)
	}
	return r.Line
}

// syncBuffer collects log output from the runtime's own goroutines as well as
// the test's.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// captureLogs redirects the default slog logger into a buffer for the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// uiRequestLine returns a named arm of Pi's published extension_ui_request
// union as a single JSONL line, read from the contract fixture the decoder
// tests assert against (server/internal/agent/pirun/testdata/pi-ui-protocol.json).
// Runtime tests use it instead of inline literals so both layers exercise the
// same transcribed wire shape — inline literals invented next to the decoder
// are what let the wrong decoder pass a green suite (KTD6).
func uiRequestLine(t *testing.T, name string) string {
	t.Helper()
	b := readProtocolFixture(t)
	var f struct {
		Requests []struct {
			Name string          `json:"name"`
			Line json.RawMessage `json:"line"`
		} `json:"requests"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("parse pi-ui-protocol fixture: %v", err)
	}
	for _, r := range f.Requests {
		if r.Name != name {
			continue
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, r.Line); err != nil {
			t.Fatalf("compact fixture line: %v", err)
		}
		return buf.String()
	}
	t.Fatalf("fixture has no request named %q", name)
	return ""
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
