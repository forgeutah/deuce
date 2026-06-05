package agent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/forgeutah/deuce/server/internal/agent/pirun"
	"github.com/forgeutah/deuce/server/internal/ws"
)

// Broadcaster delivers AgentRunEvents to a session's subscribers. *ws.Hub
// satisfies it; tests use a fake.
type Broadcaster interface {
	BroadcastToSession(sessionID string, msg ws.ServerMessage, exclude *ws.Client)
}

// Runtime ties the Pi supervisor, the persistence Store, and the WebSocket
// broadcaster into the Super Threads execution engine. It owns the per-agent
// serial queue and — per KTD12 — is the single owner of every terminal task
// transition and of promotion, including failures sourced from a process exit.
type Runtime struct {
	store Store
	sup   *pirun.Supervisor
	bc    Broadcaster

	keys keyedMutex // per-(session,agent) critical sections (KTD9 TOCTOU)

	mu         sync.Mutex
	running    map[pirun.Key]string         // current running/awaiting task id per key
	workspace  map[pirun.Key]string         // workspace id per key, for relaunch
	consumers  map[pirun.Key]*pirun.Process // process a consumer goroutine is attached to
	replies    map[string]*strings.Builder  // accumulated assistant reply per task id
	pendingReq map[string]string            // task id → pending extension_ui_request id
	timers     map[string]*taskTimers       // per-task active-work / awaiting-input timers

	activeTimeout time.Duration // active-work budget (suspended during awaiting_input)
	awaitTimeout  time.Duration // ceiling on an unanswered awaiting_input task

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

// RouteResult reports how a drawer reply was handled.
type RouteResult int

const (
	// RouteFed means the reply was delivered to a live run (steer or
	// extension_ui_response) — no new task card (R15/R17).
	RouteFed RouteResult = iota
	// RouteEnqueued means the agent was idle, so a new task was created (R19).
	RouteEnqueued
)

type taskTimers struct {
	active *time.Timer
	await  *time.Timer
}

const (
	defaultActiveTimeout = 10 * time.Minute
	defaultAwaitTimeout  = 30 * time.Minute
)

// NewRuntime builds the runtime. Call Start to begin consuming process exits.
func NewRuntime(store Store, sup *pirun.Supervisor, bc Broadcaster) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{
		store:         store,
		sup:           sup,
		bc:            bc,
		running:       make(map[pirun.Key]string),
		workspace:     make(map[pirun.Key]string),
		consumers:     make(map[pirun.Key]*pirun.Process),
		replies:       make(map[string]*strings.Builder),
		pendingReq:    make(map[string]string),
		timers:        make(map[string]*taskTimers),
		activeTimeout: defaultActiveTimeout,
		awaitTimeout:  defaultAwaitTimeout,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start launches the process-exit consumer (a supervisor exit fails the running
// task and promotes the next, KTD12).
func (r *Runtime) Start() {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case ex, ok := <-r.sup.Exits():
				if !ok {
					return
				}
				r.handleExit(ex)
			case <-r.ctx.Done():
				return
			}
		}
	}()
}

// Shutdown stops the runtime's goroutines (the supervisor is shut down by its
// owner).
func (r *Runtime) Shutdown() {
	r.cancel()
	r.wg.Wait()
}

// Enqueue creates a task and promotes it if the agent is idle. The server
// scheduler is the source of truth for order and promotion (R14).
func (r *Runtime) Enqueue(ctx context.Context, p EnqueueParams) (string, error) {
	taskID, err := r.createQueued(ctx, p)
	if err != nil {
		return "", err
	}
	r.promote(ctx, pirun.Key{SessionID: p.SessionID, AgentID: p.AgentID})
	return taskID, nil
}

// createQueued persists a queued task, records the workspace, and broadcasts
// task_enqueued. It does not promote — callers decide the locking context.
func (r *Runtime) createQueued(ctx context.Context, p EnqueueParams) (string, error) {
	taskID, seq, position, err := r.store.CreateQueuedTask(ctx, p)
	if err != nil {
		return "", err
	}
	key := pirun.Key{SessionID: p.SessionID, AgentID: p.AgentID}
	r.mu.Lock()
	r.workspace[key] = p.WorkspaceID
	r.mu.Unlock()
	r.broadcastTask(ws.TypeTaskEnqueued, ws.TaskEventPayload{
		Seq: seq, TaskID: taskID, AgentID: p.AgentID, RequestedBy: p.RequestedBy,
		AnchorMessageID: p.AnchorMessageID, Prompt: p.Prompt, State: StateQueued, Position: position,
	}, p.SessionID)
	return taskID, nil
}

// RouteOrEnqueue delivers a drawer reply atomically with the run's state, under
// the per-key lock (KTD9, closing the TOCTOU window). If the task is
// awaiting_input it answers via extension_ui_response; if merely running it
// steers; if idle/terminal it enqueues a new task (R15/R16/R17/R19).
func (r *Runtime) RouteOrEnqueue(ctx context.Context, p EnqueueParams) (RouteResult, error) {
	key := pirun.Key{SessionID: p.SessionID, AgentID: p.AgentID}
	unlock := r.keys.lock(key)
	defer unlock()

	taskID, ok := r.currentTask(key)
	if ok {
		state, sok, err := r.store.TaskState(ctx, taskID)
		if err != nil {
			return 0, err
		}
		if sok && state == StateAwaitingInput {
			// Answer the agent's blocking question (KTD15).
			reqID := r.pendingRequest(taskID)
			if err := r.sup.Send(key, pirun.ExtensionUIResponse{ID: reqID, Response: p.Prompt}); err == nil {
				seq, rerr := r.store.ResolveAwaitingInput(ctx, key.SessionID, taskID)
				if rerr == nil {
					r.clearPending(taskID)
					r.exitAwaiting(key, taskID)
					r.broadcastTask(ws.TypeTaskStarted, ws.TaskEventPayload{
						Seq: seq, TaskID: taskID, AgentID: key.AgentID, State: StateRunning,
					}, key.SessionID)
				}
				return RouteFed, nil
			}
			// Fall through to steer if the response could not be delivered.
		}
		if sok && (state == StateRunning || state == StateAwaitingInput) {
			if err := r.sup.Send(key, pirun.Steer{Message: p.Prompt}); err == nil {
				return RouteFed, nil
			}
			// Process gone between check and send — fall through to enqueue.
		}
	}

	// Idle / terminal / delivery failed: enqueue a new task and promote.
	if _, err := r.createQueued(ctx, p); err != nil {
		return 0, err
	}
	r.promoteLocked(ctx, key)
	return RouteEnqueued, nil
}

// promote takes the per-key lock and promotes the next queued task if the agent
// is idle.
func (r *Runtime) promote(ctx context.Context, key pirun.Key) {
	unlock := r.keys.lock(key)
	defer unlock()
	r.promoteLocked(ctx, key)
}

// promoteLocked assumes the per-key lock is held.
func (r *Runtime) promoteLocked(ctx context.Context, key pirun.Key) {
	if _, busy, err := r.store.RunningTask(ctx, key.SessionID, key.AgentID); err != nil {
		slog.Error("runtime: running-task lookup", "key", key.String(), "error", err)
		return
	} else if busy {
		return // one running task per key (R11)
	}
	taskID, prompt, ok, err := r.store.NextQueuedTask(ctx, key.SessionID, key.AgentID)
	if err != nil {
		slog.Error("runtime: next-queued lookup", "key", key.String(), "error", err)
		return
	}
	if !ok {
		return
	}

	seq, err := r.store.MarkRunning(ctx, key.SessionID, taskID)
	if err != nil {
		slog.Error("runtime: mark running", "task", taskID, "error", err)
		return
	}
	r.setRunning(key, taskID)
	r.broadcastTask(ws.TypeTaskStarted, ws.TaskEventPayload{
		Seq: seq, TaskID: taskID, AgentID: key.AgentID, State: StateRunning,
	}, key.SessionID)

	// Ensure the Pi process + its consumer, then send the prompt. Continuity
	// across a process restart (pi_session_id re-attach) is a tolerated v1
	// degradation — within a process, sequential tasks share the Pi session.
	r.mu.Lock()
	wsID := r.workspace[key]
	r.mu.Unlock()
	p, err := r.sup.Ensure(ctx, key, wsID, "")
	if err != nil {
		slog.Error("runtime: ensure pi process", "key", key.String(), "error", err)
		r.terminalLocked(ctx, key, taskID, StateFailed, "Agent process could not start.")
		return
	}
	r.attachConsumer(key, p)
	if err := p.Send(pirun.Prompt{Message: prompt, ID: taskID}); err != nil {
		slog.Error("runtime: send prompt", "key", key.String(), "error", err)
		r.terminalLocked(ctx, key, taskID, StateFailed, "Failed to send prompt to agent.")
		return
	}
	r.startActive(key, taskID)
}

// attachConsumer starts a per-process event-translation goroutine if one is not
// already running for this process instance.
func (r *Runtime) attachConsumer(key pirun.Key, p *pirun.Process) {
	r.mu.Lock()
	if r.consumers[key] == p {
		r.mu.Unlock()
		return
	}
	r.consumers[key] = p
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case ev, ok := <-p.Events():
				if !ok {
					r.mu.Lock()
					if r.consumers[key] == p {
						delete(r.consumers, key)
					}
					r.mu.Unlock()
					return
				}
				r.translate(key, ev)
			case <-r.ctx.Done():
				// Runtime shutdown: stop consuming even though the process is
				// still alive (the supervisor tears it down separately).
				return
			}
		}
	}()
}

// translate maps one decoded Pi event to persistence + a broadcast, attributed
// to the key's current running task.
func (r *Runtime) translate(key pirun.Key, ev pirun.Event) {
	taskID, ok := r.currentTask(key)
	if !ok {
		return // no running task to attribute to
	}
	ctx := r.ctx
	switch ev.Kind {
	case pirun.KindToolStarted:
		seq, err := r.store.AppendActionStarted(ctx, key.SessionID, taskID, ev.ToolCallID, ev.Tool, ev.Arg)
		if err != nil {
			slog.Error("runtime: append action", "task", taskID, "error", err)
			return
		}
		r.broadcastAction(ws.TypeActionStarted, ws.ActionEventPayload{
			Seq: seq, TaskID: taskID, AgentID: key.AgentID, CallID: ev.ToolCallID, Tool: ev.Tool, Arg: ev.Arg,
		}, key.SessionID)
	case pirun.KindToolCompleted:
		seq, err := r.store.CompleteAction(ctx, key.SessionID, taskID, ev.ToolCallID, ev.Output, ev.IsError)
		if err != nil {
			slog.Error("runtime: complete action", "task", taskID, "error", err)
			return
		}
		r.broadcastAction(ws.TypeActionCompleted, ws.ActionEventPayload{
			Seq: seq, TaskID: taskID, AgentID: key.AgentID, CallID: ev.ToolCallID, Tool: ev.Tool, Text: ev.Output, IsError: ev.IsError,
		}, key.SessionID)
	case pirun.KindAssistantText:
		r.appendReply(taskID, ev.Text)
	case pirun.KindAwaitingInput:
		seq, err := r.store.SetAwaitingInput(ctx, key.SessionID, taskID, ev.Prompt)
		if err != nil {
			slog.Error("runtime: set awaiting input", "task", taskID, "error", err)
			return
		}
		r.setPending(taskID, ev.RequestID)
		r.enterAwaiting(key, taskID) // suspend active timeout, start ceiling (KTD8)
		r.broadcastTask(ws.TypeTaskAwaitingInput, ws.TaskEventPayload{
			Seq: seq, TaskID: taskID, AgentID: key.AgentID, State: StateAwaitingInput, PendingQuestion: ev.Prompt,
		}, key.SessionID)
	case pirun.KindRunCompleted:
		unlock := r.keys.lock(key)
		r.terminalLocked(ctx, key, taskID, StateDone, r.takeReply(taskID))
		unlock()
	}
}

// handleExit fails the running task for an exited process and promotes the next.
func (r *Runtime) handleExit(ex pirun.Exit) {
	unlock := r.keys.lock(ex.Key)
	defer unlock()
	taskID, ok := r.currentTask(ex.Key)
	if !ok {
		return
	}
	reply := "Agent process exited unexpectedly."
	if ex.Err == nil {
		reply = "" // clean exit with no run completion still terminates the task
	}
	r.terminalLocked(r.ctx, ex.Key, taskID, StateFailed, reply)
}

// terminalLocked performs an idempotent terminal transition and promotes the
// next queued task. Assumes the per-key lock is held (KTD12).
func (r *Runtime) terminalLocked(ctx context.Context, key pirun.Key, taskID, state, reply string) {
	cur, ok, err := r.store.TaskState(ctx, taskID)
	if err != nil {
		slog.Error("runtime: task-state lookup", "task", taskID, "error", err)
		return
	}
	if ok && isTerminal(cur) {
		return // already terminal — second signal is a no-op (idempotent, KTD12)
	}
	seq, err := r.store.FinishTask(ctx, key.SessionID, taskID, state, reply)
	if err != nil {
		slog.Error("runtime: finish task", "task", taskID, "state", state, "error", err)
		return
	}
	r.stopTimers(taskID)
	r.clearPending(taskID)
	r.clearRunning(key, taskID)
	r.broadcastTask(ws.TypeTaskCompleted, ws.TaskEventPayload{
		Seq: seq, TaskID: taskID, AgentID: key.AgentID, State: state, Status: state, Reply: reply,
	}, key.SessionID)
	// Promote the next queued task in the same critical section so the UI never
	// shows idle-with-queued (AE4/R13).
	r.promoteLocked(ctx, key)
}

// Cancel cancels the running task for a key (/stop targeting an agent, R21).
func (r *Runtime) Cancel(ctx context.Context, key pirun.Key) {
	unlock := r.keys.lock(key)
	taskID, ok := r.currentTask(key)
	if ok {
		r.terminalLocked(ctx, key, taskID, StateCancelled, "Cancelled by user.")
	}
	unlock()
	if ok {
		r.sup.Stop(key) // tear down the Pi process so it stops emitting (KTD6a)
	}
}

// --- small state helpers (guarded by r.mu) ---

func (r *Runtime) setRunning(key pirun.Key, taskID string) {
	r.mu.Lock()
	r.running[key] = taskID
	r.mu.Unlock()
}

func (r *Runtime) clearRunning(key pirun.Key, taskID string) {
	r.mu.Lock()
	if r.running[key] == taskID {
		delete(r.running, key)
	}
	r.mu.Unlock()
}

func (r *Runtime) currentTask(key pirun.Key) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.running[key]
	return id, ok
}

func (r *Runtime) appendReply(taskID, delta string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.replies[taskID]
	if b == nil {
		b = &strings.Builder{}
		r.replies[taskID] = b
	}
	b.WriteString(delta)
}

func (r *Runtime) takeReply(taskID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.replies[taskID]
	delete(r.replies, taskID)
	if b == nil {
		return ""
	}
	return b.String()
}

func (r *Runtime) setPending(taskID, reqID string) {
	r.mu.Lock()
	r.pendingReq[taskID] = reqID
	r.mu.Unlock()
}

func (r *Runtime) pendingRequest(taskID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pendingReq[taskID]
}

func (r *Runtime) clearPending(taskID string) {
	r.mu.Lock()
	delete(r.pendingReq, taskID)
	r.mu.Unlock()
}

// failTaskAsync fails a task from a timer callback, but only if it is still the
// key's current task (terminalLocked is idempotent, so a late timer is safe).
func (r *Runtime) failTaskAsync(key pirun.Key, taskID, reply string) {
	unlock := r.keys.lock(key)
	defer unlock()
	if cur, ok := r.currentTask(key); ok && cur == taskID {
		r.terminalLocked(r.ctx, key, taskID, StateFailed, reply)
	}
}

// startActive starts the active-work timeout for a running task.
func (r *Runtime) startActive(key pirun.Key, taskID string) {
	if r.activeTimeout <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tt := r.timerFor(taskID)
	if tt.active != nil {
		tt.active.Stop()
	}
	tt.active = time.AfterFunc(r.activeTimeout, func() { r.failTaskAsync(key, taskID, "Agent timed out.") })
}

// enterAwaiting suspends the active-work timeout and starts the awaiting-input
// ceiling so an unanswered question cannot wedge the agent forever (KTD8).
func (r *Runtime) enterAwaiting(key pirun.Key, taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tt := r.timerFor(taskID)
	if tt.active != nil {
		tt.active.Stop()
		tt.active = nil
	}
	if r.awaitTimeout > 0 {
		if tt.await != nil {
			tt.await.Stop()
		}
		tt.await = time.AfterFunc(r.awaitTimeout, func() {
			r.failTaskAsync(key, taskID, "No response to the agent's question.")
		})
	}
}

// exitAwaiting cancels the ceiling and resumes the active-work timeout.
func (r *Runtime) exitAwaiting(key pirun.Key, taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tt := r.timers[taskID]
	if tt == nil {
		return
	}
	if tt.await != nil {
		tt.await.Stop()
		tt.await = nil
	}
	if r.activeTimeout > 0 {
		tt.active = time.AfterFunc(r.activeTimeout, func() { r.failTaskAsync(key, taskID, "Agent timed out.") })
	}
}

func (r *Runtime) stopTimers(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tt := r.timers[taskID]; tt != nil {
		if tt.active != nil {
			tt.active.Stop()
		}
		if tt.await != nil {
			tt.await.Stop()
		}
		delete(r.timers, taskID)
	}
}

// timerFor returns (creating if needed) the timer set for a task. Caller holds r.mu.
func (r *Runtime) timerFor(taskID string) *taskTimers {
	tt := r.timers[taskID]
	if tt == nil {
		tt = &taskTimers{}
		r.timers[taskID] = tt
	}
	return tt
}

func (r *Runtime) broadcastTask(typ string, p ws.TaskEventPayload, sessionID string) {
	msg, err := ws.NewServerMessage(typ, sessionID, p)
	if err != nil {
		slog.Error("runtime: marshal task event", "type", typ, "error", err)
		return
	}
	r.bc.BroadcastToSession(sessionID, msg, nil)
}

func (r *Runtime) broadcastAction(typ string, p ws.ActionEventPayload, sessionID string) {
	msg, err := ws.NewServerMessage(typ, sessionID, p)
	if err != nil {
		slog.Error("runtime: marshal action event", "type", typ, "error", err)
		return
	}
	r.bc.BroadcastToSession(sessionID, msg, nil)
}

// keyedMutex provides a mutex per key so transitions on distinct (session,
// agent) keys run concurrently while a single key stays serial.
type keyedMutex struct {
	mu sync.Mutex
	m  map[pirun.Key]*sync.Mutex
}

func (k *keyedMutex) lock(key pirun.Key) func() {
	k.mu.Lock()
	if k.m == nil {
		k.m = make(map[pirun.Key]*sync.Mutex)
	}
	mu, ok := k.m[key]
	if !ok {
		mu = &sync.Mutex{}
		k.m[key] = mu
	}
	k.mu.Unlock()
	mu.Lock()
	return mu.Unlock
}
