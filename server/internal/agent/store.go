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
	// question and returns the event seq. kind is the question type (input /
	// select / confirm; empty means free-text input) and options are the choice
	// labels for a select question (nil otherwise).
	SetAwaitingInput(ctx context.Context, sessionID, taskID, question, kind string, options []string) (seq int64, err error)

	// ResolveAwaitingInput transitions awaiting_input→running and returns the seq.
	ResolveAwaitingInput(ctx context.Context, sessionID, taskID string) (seq int64, err error)

	// FinishTask marks a terminal state (done/failed/cancelled), persists the
	// reply, force-resolves any still-open actions, and returns the event seq.
	FinishTask(ctx context.Context, sessionID, taskID, state, reply string) (seq int64, err error)

	// RunningTask returns the session's running (or awaiting_input) task, if any.
	RunningTask(ctx context.Context, sessionID string) (taskID string, ok bool, err error)

	// NextQueuedTask returns the session's oldest queued task, if any.
	NextQueuedTask(ctx context.Context, sessionID string) (taskID, prompt string, ok bool, err error)

	// QueuedTaskIDs returns every queued task in the session, oldest first.
	// Backs CancelSession's queue drain (R6).
	QueuedTaskIDs(ctx context.Context, sessionID string) ([]string, error)

	// TaskState returns the current state of a task.
	TaskState(ctx context.Context, taskID string) (state string, ok bool, err error)

	// DeuceSystemPrompt returns deuce's configured system prompt (empty when
	// unset). Applied to the Pi process at launch via --append-system-prompt.
	DeuceSystemPrompt(ctx context.Context) (string, error)
}

// DeuceAgentID is the fixed UUID of the single built-in deuce agent. It MUST
// match the row seeded by migration 013_single_deuce_agent.sql and the DEUCE
// constant in src/lib/deuce.ts — message authorship, the chat visibility
// filter, and the migration's historical repoint all pin to it. The nil UUID
// stays reserved as the system-notice author sentinel.
const DeuceAgentID = "00000000-0000-0000-0000-00000000000d"

// DeuceAgentName is the agent's name — the @mention token the server detects
// and the display name the frontend DEUCE constant mirrors. The DB row does
// not carry a name; this constant is the single server-side source.
const DeuceAgentName = "deuce"

// EnqueueParams describes a new task to enqueue.
type EnqueueParams struct {
	SessionID       string
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
