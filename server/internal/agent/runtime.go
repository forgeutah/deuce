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
// broadcaster into the Super Threads execution engine. It owns the per-session
// serial queue and — per KTD12 — is the single owner of every terminal task
// transition and of promotion, including failures sourced from a process exit.
type Runtime struct {
	store Store
	sup   *pirun.Supervisor
	bc    Broadcaster
	// baseSystemPrompt is prepended to deuce's configurable system_prompt at Pi
	// launch — the global "prefer ask_user when blocked" guidance.
	baseSystemPrompt string
	// replyPoster, when set, posts deuce's terminal reply as a normal chat
	// message so it shows in the existing chat UI (the Super Threads task/action
	// cards are a separate, later surface). Wired by the handler layer.
	replyPoster func(sessionID, reply string)

	keys keyedMutex // per-session critical sections (KTD9 TOCTOU)

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

// DefaultBaseSystemPrompt is the global system prompt applied to the deuce
// agent at Pi launch, ahead of its configurable system_prompt. Its load-bearing
// job is to steer the agent to the ask_user tool when blocked on a human
// decision (which is what surfaces the interactive typed prompt) instead of
// guessing or asking in a normal chat reply. Overridable via
// DEUCE_AGENT_SYSTEM_PROMPT.
const DefaultBaseSystemPrompt = `You are deuce, an AI agent working in a shared Deuce workspace alongside your human teammates. Your messages appear in a chat channel.

CRITICAL — how to ask the user something. The ONLY reliable way to get an answer from a human is the ask_user tool. It pauses your run, shows the user a real prompt, and returns their answer to you so you can continue. If you instead write a question as ordinary chat text and end your turn, your run is marked complete — you are NOT waiting, the user is not shown a prompt, and you will not receive a clean answer to act on. A plain-text question is a dead end.

Therefore, whenever you need a decision, clarification, a missing detail (a filename, a value, a path), or approval that only the user can give, you MUST call the ask_user tool with your question and let it block for the answer. Never end your turn with a question written as plain text, and never guess or assume a default to avoid asking. Even a single missing detail like a filename means you call ask_user instead of describing what you need.

Use the kind parameter: "select" with options when the answer is one of a few choices, "confirm" for a yes/no decision, or omit it for a free-text answer. Only ask when you are genuinely blocked — otherwise keep working without asking.`

// NewRuntime builds the runtime. Call Start to begin consuming process exits.
// baseSystemPrompt is prepended to deuce's configurable system_prompt at Pi
// launch (pass DefaultBaseSystemPrompt for the standard guidance, or "" to
// disable).
func NewRuntime(store Store, sup *pirun.Supervisor, bc Broadcaster, baseSystemPrompt string) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{
		store:            store,
		sup:              sup,
		bc:               bc,
		baseSystemPrompt: baseSystemPrompt,
		running:          make(map[pirun.Key]string),
		workspace:        make(map[pirun.Key]string),
		consumers:        make(map[pirun.Key]*pirun.Process),
		replies:          make(map[string]*strings.Builder),
		pendingReq:       make(map[string]string),
		timers:           make(map[string]*taskTimers),
		activeTimeout:    defaultActiveTimeout,
		awaitTimeout:     defaultAwaitTimeout,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// joinSystemPrompts combines the global base prompt with deuce's configurable
// system_prompt, trimming each and separating with a blank line. Either may be
// empty, in which case the other is returned (both empty → "").
func joinSystemPrompts(base, agent string) string {
	base = strings.TrimSpace(base)
	agent = strings.TrimSpace(agent)
	switch {
	case base == "":
		return agent
	case agent == "":
		return base
	default:
		return base + "\n\n" + agent
	}
}

// SetReplyPoster installs a callback that posts deuce's terminal reply as a
// chat message. Without it, agent output is only visible via the AgentRunEvent
// stream (the Super Threads cards), not in the existing chat.
func (r *Runtime) SetReplyPoster(fn func(sessionID, reply string)) {
	r.replyPoster = fn
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
	slog.Info("runtime: task enqueued", "session", p.SessionID, "task", taskID, "workspace", p.WorkspaceID)
	r.promote(ctx, pirun.Key(p.SessionID))
	return taskID, nil
}

// createQueued persists a queued task, records the workspace, and broadcasts
// task_enqueued. It does not promote — callers decide the locking context.
func (r *Runtime) createQueued(ctx context.Context, p EnqueueParams) (string, error) {
	taskID, seq, position, err := r.store.CreateQueuedTask(ctx, p)
	if err != nil {
		return "", err
	}
	key := pirun.Key(p.SessionID)
	r.mu.Lock()
	r.workspace[key] = p.WorkspaceID
	r.mu.Unlock()
	r.broadcastTask(ws.TypeTaskEnqueued, ws.TaskEventPayload{
		Seq: seq, TaskID: taskID, RequestedBy: p.RequestedBy,
		AnchorMessageID: p.AnchorMessageID, Prompt: p.Prompt, State: StateQueued, Position: position,
	}, p.SessionID)
	return taskID, nil
}

// RouteOrEnqueue delivers a drawer reply atomically with the run's state, under
// the per-key lock (KTD9, closing the TOCTOU window). If the task is
// awaiting_input it answers via extension_ui_response; if merely running it
// steers; if idle/terminal it enqueues a new task (R15/R16/R17/R19).
func (r *Runtime) RouteOrEnqueue(ctx context.Context, p EnqueueParams) (RouteResult, error) {
	key := pirun.Key(p.SessionID)
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
				// The run has resumed in-process — always tear down the awaiting
				// ceiling and pending state so it can't later fail a live task,
				// even if the DB resolve below fails (the next event reconciles).
				r.clearPending(taskID)
				r.exitAwaiting(key, taskID)
				seq, rerr := r.store.ResolveAwaitingInput(ctx, string(key), taskID)
				if rerr != nil {
					slog.Error("runtime: resolve awaiting input", "task", taskID, "error", rerr)
				} else {
					r.broadcastTask(ws.TypeTaskStarted, ws.TaskEventPayload{
						Seq: seq, TaskID: taskID, State: StateRunning,
					}, string(key))
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
	if _, busy, err := r.store.RunningTask(ctx, string(key)); err != nil {
		slog.Error("runtime: running-task lookup", "key", key.String(), "error", err)
		return
	} else if busy {
		return // one running task per session (R11)
	}
	taskID, prompt, ok, err := r.store.NextQueuedTask(ctx, string(key))
	if err != nil {
		slog.Error("runtime: next-queued lookup", "key", key.String(), "error", err)
		return
	}
	if !ok {
		return
	}

	seq, err := r.store.MarkRunning(ctx, string(key), taskID)
	if err != nil {
		slog.Error("runtime: mark running", "task", taskID, "error", err)
		return
	}
	r.setRunning(key, taskID)
	r.broadcastTask(ws.TypeTaskStarted, ws.TaskEventPayload{
		Seq: seq, TaskID: taskID, State: StateRunning,
	}, string(key))

	// Ensure the Pi process + its consumer, then send the prompt. Continuity
	// across a process restart is a tolerated v1 degradation — within a
	// process, sequential tasks share the Pi session.
	r.mu.Lock()
	wsID := r.workspace[key]
	r.mu.Unlock()
	// The global guidance plus deuce's configurable instructions are applied
	// to the Pi process at launch (Ensure only launches when no process exists
	// for the key, so this is a no-op on reuse — a prompt edit takes effect on
	// the next launch; see RecycleIdleProcesses). A lookup failure is
	// non-fatal — fall back to the global base alone rather than failing the task.
	deucePrompt, err := r.store.DeuceSystemPrompt(ctx)
	if err != nil {
		slog.Warn("runtime: deuce system prompt lookup failed", "key", key.String(), "error", err)
		deucePrompt = ""
	}
	systemPrompt := joinSystemPrompts(r.baseSystemPrompt, deucePrompt)
	p, err := r.sup.Ensure(ctx, key, wsID, systemPrompt)
	if err != nil {
		slog.Error("runtime: ensure pi process", "key", key.String(), "error", err)
		// promote=false: we are inside promoteLocked, don't recurse. teardown=true
		// so the next queued task is promoted by the resulting process-exit.
		r.finalizeLocked(ctx, key, taskID, StateFailed, "Agent process could not start.", false, true)
		return
	}
	r.attachConsumer(key, p)
	if err := p.Send(pirun.Prompt{Message: prompt, ID: taskID}); err != nil {
		slog.Error("runtime: send prompt", "key", key.String(), "error", err)
		r.finalizeLocked(ctx, key, taskID, StateFailed, "Failed to send prompt to agent.", false, true)
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
		seq, err := r.store.AppendActionStarted(ctx, string(key), taskID, ev.ToolCallID, ev.Tool, ev.Arg)
		if err != nil {
			slog.Error("runtime: append action", "task", taskID, "error", err)
			return
		}
		r.broadcastAction(ws.TypeActionStarted, ws.ActionEventPayload{
			Seq: seq, TaskID: taskID, CallID: ev.ToolCallID, Tool: ev.Tool, Arg: ev.Arg,
		}, string(key))
	case pirun.KindToolCompleted:
		seq, err := r.store.CompleteAction(ctx, string(key), taskID, ev.ToolCallID, ev.Output, ev.IsError)
		if err != nil {
			slog.Error("runtime: complete action", "task", taskID, "error", err)
			return
		}
		r.broadcastAction(ws.TypeActionCompleted, ws.ActionEventPayload{
			Seq: seq, TaskID: taskID, CallID: ev.ToolCallID, Tool: ev.Tool, Text: ev.Output, IsError: ev.IsError,
		}, string(key))
	case pirun.KindAssistantText:
		r.appendReply(taskID, ev.Text)
	case pirun.KindAwaitingInput:
		seq, err := r.store.SetAwaitingInput(ctx, string(key), taskID, ev.Prompt, ev.RequestKind, ev.Options)
		if err != nil {
			slog.Error("runtime: set awaiting input", "task", taskID, "error", err)
			return
		}
		r.setPending(taskID, ev.RequestID)
		r.enterAwaiting(key, taskID) // suspend active timeout, start ceiling (KTD8)
		r.broadcastTask(ws.TypeTaskAwaitingInput, ws.TaskEventPayload{
			Seq: seq, TaskID: taskID, State: StateAwaitingInput,
			PendingQuestion: ev.Prompt, PendingQuestionKind: ev.RequestKind, PendingQuestionOptions: ev.Options,
		}, string(key))
	case pirun.KindRunCompleted:
		unlock := r.keys.lock(key)
		// Pi finished cleanly: reuse the live process for the next task (promote
		// inline), no teardown.
		r.finalizeLocked(ctx, key, taskID, StateDone, r.takeReply(taskID), true, false)
		unlock()
	}
}

// handleExit fails the running task for an exited process and promotes the next.
func (r *Runtime) handleExit(ex pirun.Exit) {
	unlock := r.keys.lock(ex.Key)
	defer unlock()
	taskID, ok := r.currentTask(ex.Key)
	if !ok {
		// No task bound to this process (e.g. it was torn down by a cancel/
		// timeout that already finalized the task). Promote the next queued task
		// onto a fresh process now that this one is fully gone.
		r.promoteLocked(r.ctx, ex.Key)
		return
	}
	reply := "Agent process exited unexpectedly."
	if ex.Err == nil {
		reply = "" // clean exit with no run completion still terminates the task
	}
	// The process is already dead and removed; promote the next onto a fresh
	// process (promote=true), no teardown needed.
	r.finalizeLocked(r.ctx, ex.Key, taskID, StateFailed, reply, true, false)
}

// finalizeLocked performs an idempotent terminal transition. Assumes the per-key
// lock is held (KTD12).
//
//   - teardown: tear down the task's Pi process (KTD6a) — set for cancel/timeout/
//     failed-start where the live process must not outlive its task. Promotion of
//     the next task is then deferred to the resulting process-exit (handleExit),
//     which runs after the supervisor has fully removed the dead process, so the
//     next task launches on a fresh process rather than racing the dying one.
//   - promote: promote the next queued task inline (set for the Pi-`done` and
//     crash paths, where the process is reused or already gone). The promoted
//     task's task_started is broadcast BEFORE this task's task_completed so the
//     UI never observes idle-with-queued (AE4/R13).
func (r *Runtime) finalizeLocked(ctx context.Context, key pirun.Key, taskID, state, reply string, promote, teardown bool) {
	cur, ok, err := r.store.TaskState(ctx, taskID)
	if err != nil {
		slog.Error("runtime: task-state lookup", "task", taskID, "error", err)
		return
	}
	if ok && isTerminal(cur) {
		return // already terminal — second signal is a no-op (idempotent, KTD12)
	}
	// No-JSON backstop: if the agent narrated an ask_user tool call as text
	// (the ask-user extension didn't fire), rewrite it to the plain question
	// before it is persisted, broadcast, and posted to chat (R9/R11/R12).
	reply = sanitizeNarratedQuestion(reply)
	seq, err := r.store.FinishTask(ctx, string(key), taskID, state, reply)
	if err != nil {
		slog.Error("runtime: finish task", "task", taskID, "state", state, "error", err)
		return
	}
	r.stopTimers(taskID)
	r.clearPending(taskID)
	r.clearRunning(key, taskID)
	if teardown {
		r.sup.Stop(key) // KTD6a: no live, file-mutating process outlives its task
	}
	if promote {
		r.promoteLocked(ctx, key)
	}
	r.broadcastTask(ws.TypeTaskCompleted, ws.TaskEventPayload{
		Seq: seq, TaskID: taskID, State: state, Status: state, Reply: reply,
	}, string(key))
	slog.Info("runtime: task finalized", "key", key.String(), "task", taskID, "state", state, "replyLen", len(reply))
	// Surface the result in the existing chat. For a done task with no streamed
	// text (e.g. the model produced only tool calls, or the run errored without
	// a reply), post a fallback so an @mention never silently produces nothing.
	if r.replyPoster != nil {
		msg := reply
		if msg == "" && state == StateDone {
			msg = "(The agent finished without a text response.)"
		}
		if msg != "" {
			r.replyPoster(string(key), msg)
		}
	}
}

// CancelSession cancels the session's running task AND its queued tasks (R6:
// /stop or the Stop button halts everything; a queued task must not be
// promoted seconds after the user asked for quiet).
func (r *Runtime) CancelSession(ctx context.Context, sessionID string) {
	key := pirun.Key(sessionID)
	unlock := r.keys.lock(key)
	defer unlock()
	r.cancelQueuedLocked(ctx, key)
	taskID, ok := r.currentTask(key)
	if ok {
		// Teardown=true: the resulting process-exit would promote the next
		// queued task onto a fresh process — but the queue was just drained.
		r.finalizeLocked(ctx, key, taskID, StateCancelled, "Cancelled by user.", false, true)
	}
}

// cancelQueuedLocked finalizes every queued task in the session as cancelled,
// broadcasting each task_completed. No replyPoster — one "Cancelled by user."
// chat notice for the running task is enough; queued-card state changes carry
// the rest. Assumes the per-key lock is held.
func (r *Runtime) cancelQueuedLocked(ctx context.Context, key pirun.Key) {
	ids, err := r.store.QueuedTaskIDs(ctx, string(key))
	if err != nil {
		slog.Error("runtime: queued-task lookup", "key", key.String(), "error", err)
		return
	}
	for _, taskID := range ids {
		seq, err := r.store.FinishTask(ctx, string(key), taskID, StateCancelled, "")
		if err != nil {
			slog.Error("runtime: cancel queued task", "task", taskID, "error", err)
			continue
		}
		r.broadcastTask(ws.TypeTaskCompleted, ws.TaskEventPayload{
			Seq: seq, TaskID: taskID, State: StateCancelled, Status: StateCancelled,
		}, string(key))
	}
}

// RecycleIdleProcesses stops the Pi process of every session with no running or
// awaiting_input task, so the next task launches with the freshly-saved system
// prompt (R7). Sessions mid-task keep their process and pick the new prompt up
// on their next process launch. StopAndForget deregisters synchronously under
// the per-session lock, so an enqueue racing the recycle launches a fresh
// process instead of adopting the dying one.
func (r *Runtime) RecycleIdleProcesses() {
	for _, key := range r.sup.LiveKeys() {
		unlock := r.keys.lock(key)
		if _, busy := r.currentTask(key); !busy {
			r.sup.StopAndForget(key)
		}
		unlock()
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
		// Timeout: the live process is stuck — tear it down; the resulting exit
		// promotes the next queued task.
		r.finalizeLocked(r.ctx, key, taskID, StateFailed, reply, false, true)
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

// keyedMutex provides a mutex per session key so transitions on distinct
// sessions run concurrently while a single session stays serial.
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
