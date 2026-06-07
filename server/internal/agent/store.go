package agent

import "context"

// Store is the persistence seam the Runtime depends on. Every event-bearing
// method allocates the per-session event seq and writes the state change in one
// transaction (KTD6 persist-first/in-tx-seq), returning the committed seq the
// caller then broadcasts. Queue/lookup methods carry no event and return no seq.
//
// The DB implementation lives in dbstore.go; tests use an in-memory fake so the
// orchestration logic (promotion, idempotent terminal transitions, seq
// ordering) is exercised without Postgres.
type Store interface {
	// CreateQueuedTask inserts a task in state "queued" and returns its id, the
	// allocated event seq, and its 1-based queue position among the agent's
	// queued tasks.
	CreateQueuedTask(ctx context.Context, p EnqueueParams) (taskID string, seq int64, position int, err error)

	// MarkRunning transitions queued→running and returns the event seq.
	MarkRunning(ctx context.Context, sessionID, taskID string) (seq int64, err error)

	// AppendActionStarted records a tool-call start (idempotent on call id) and
	// returns the event seq.
	AppendActionStarted(ctx context.Context, sessionID, taskID, callID, tool, arg string) (seq int64, err error)

	// CompleteAction resolves a tool call and returns the event seq.
	CompleteAction(ctx context.Context, sessionID, taskID, callID, text string, isError bool) (seq int64, err error)

	// SetAwaitingInput transitions running→awaiting_input with the pending
	// question and returns the event seq.
	SetAwaitingInput(ctx context.Context, sessionID, taskID, question string) (seq int64, err error)

	// ResolveAwaitingInput transitions awaiting_input→running and returns the seq.
	ResolveAwaitingInput(ctx context.Context, sessionID, taskID string) (seq int64, err error)

	// FinishTask marks a terminal state (done/failed/cancelled), persists the
	// reply, force-resolves any still-open actions, and returns the event seq.
	FinishTask(ctx context.Context, sessionID, taskID, state, reply string) (seq int64, err error)

	// RunningTask returns the running (or awaiting_input) task for a key, if any.
	RunningTask(ctx context.Context, sessionID, agentID string) (taskID string, ok bool, err error)

	// NextQueuedTask returns the oldest queued task for a key, if any.
	NextQueuedTask(ctx context.Context, sessionID, agentID string) (taskID, prompt string, ok bool, err error)

	// TaskState returns the current state of a task.
	TaskState(ctx context.Context, taskID string) (state string, ok bool, err error)
}

// EnqueueParams describes a new task to enqueue.
type EnqueueParams struct {
	SessionID       string
	AgentID         string
	RequestedBy     string
	AnchorMessageID string
	Prompt          string
	WorkspaceID     string
}

// Terminal task states.
const (
	StateQueued        = "queued"
	StateRunning       = "running"
	StateAwaitingInput = "awaiting_input"
	StateDone          = "done"
	StateFailed        = "failed"
	StateCancelled     = "cancelled"
)

func isTerminal(state string) bool {
	return state == StateDone || state == StateFailed || state == StateCancelled
}
