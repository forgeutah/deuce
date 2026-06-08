package agent

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// DBStore is the production Store backed by Postgres. Each event-bearing method
// allocates the per-session seq and writes the state change in ONE transaction
// (KTD6), so the persisted seq and the broadcast seq are always the same value
// and survive restart.
type DBStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func NewDBStore(pool *pgxpool.Pool, q *db.Queries) *DBStore {
	return &DBStore{pool: pool, q: q}
}

// withSeq runs fn inside a transaction after allocating the session's next event
// seq, returning that committed seq for the caller to broadcast.
func (s *DBStore) withSeq(ctx context.Context, sessionID string, fn func(q *db.Queries, seq int64) error) (int64, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return 0, fmt.Errorf("parse session id: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	seq, err := q.AllocateEventSeq(ctx, sid)
	if err != nil {
		return 0, fmt.Errorf("allocate seq: %w", err)
	}
	if err := fn(q, seq); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return seq, nil
}

func (s *DBStore) CreateQueuedTask(ctx context.Context, p EnqueueParams) (string, int64, int, error) {
	sid, err := uuid.Parse(p.SessionID)
	if err != nil {
		return "", 0, 0, fmt.Errorf("parse session id: %w", err)
	}
	aid, err := uuid.Parse(p.AgentID)
	if err != nil {
		return "", 0, 0, fmt.Errorf("parse agent id: %w", err)
	}

	var taskID string
	seq, err := s.withSeq(ctx, p.SessionID, func(q *db.Queries, seq int64) error {
		task, err := q.CreateTask(ctx, db.CreateTaskParams{
			SessionID:       sid,
			AgentID:         aid,
			RequestedBy:     parseNullableUUID(p.RequestedBy),
			AnchorMessageID: parseNullableUUID(p.AnchorMessageID),
			Prompt:          p.Prompt,
			State:           StateQueued,
			Seq:             seq,
		})
		if err != nil {
			return err
		}
		taskID = task.ID.String()
		return nil
	})
	if err != nil {
		return "", 0, 0, err
	}

	// Queue position: 1-based index among the agent's queued tasks.
	position := 1
	if tasks, err := s.q.ListAgentTasks(ctx, db.ListAgentTasksParams{SessionID: sid, AgentID: aid}); err == nil {
		n := 0
		for _, t := range tasks {
			if t.State == StateQueued {
				n++
				if t.ID.String() == taskID {
					position = n
					break
				}
			}
		}
	}
	return taskID, seq, position, nil
}

func (s *DBStore) MarkRunning(ctx context.Context, sessionID, taskID string) (int64, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return 0, err
	}
	return s.withSeq(ctx, sessionID, func(q *db.Queries, seq int64) error {
		return q.UpdateTaskState(ctx, db.UpdateTaskStateParams{ID: tid, State: StateRunning, Seq: seq})
	})
}

func (s *DBStore) AppendActionStarted(ctx context.Context, sessionID, taskID, callID, tool, arg string) (int64, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return 0, err
	}
	return s.withSeq(ctx, sessionID, func(q *db.Queries, seq int64) error {
		return q.AppendAction(ctx, db.AppendActionParams{TaskID: tid, CallID: callID, Seq: seq, Tool: tool, Arg: arg})
	})
}

func (s *DBStore) CompleteAction(ctx context.Context, sessionID, taskID, callID, text string, isError bool) (int64, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return 0, err
	}
	status := "completed"
	if isError {
		status = "error"
	}
	return s.withSeq(ctx, sessionID, func(q *db.Queries, seq int64) error {
		return q.CompleteAction(ctx, db.CompleteActionParams{
			TaskID: tid, CallID: callID, Status: status, Text: text, Seq: seq,
		})
	})
}

func (s *DBStore) SetAwaitingInput(ctx context.Context, sessionID, taskID, question, kind string, options []string) (int64, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return 0, err
	}
	if options == nil {
		options = []string{}
	}
	return s.withSeq(ctx, sessionID, func(q *db.Queries, seq int64) error {
		return q.SetTaskAwaitingInput(ctx, db.SetTaskAwaitingInputParams{
			ID:                     tid,
			PendingQuestion:        question,
			PendingQuestionKind:    kind,
			PendingQuestionOptions: options,
			Seq:                    seq,
		})
	})
}

func (s *DBStore) ResolveAwaitingInput(ctx context.Context, sessionID, taskID string) (int64, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return 0, err
	}
	return s.withSeq(ctx, sessionID, func(q *db.Queries, seq int64) error {
		return q.ResolveTaskInput(ctx, db.ResolveTaskInputParams{ID: tid, Seq: seq})
	})
}

func (s *DBStore) FinishTask(ctx context.Context, sessionID, taskID, state, reply string) (int64, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return 0, err
	}
	return s.withSeq(ctx, sessionID, func(q *db.Queries, seq int64) error {
		// Force-resolve still-open actions in the same tx so the snapshot can
		// never show a spinning action after the task is terminal.
		if err := q.ForceResolveOpenActions(ctx, tid); err != nil {
			return err
		}
		return q.FinishTask(ctx, db.FinishTaskParams{ID: tid, State: state, Reply: reply, Work: nil, Seq: seq})
	})
}

func (s *DBStore) RunningTask(ctx context.Context, sessionID, agentID string) (string, bool, error) {
	tasks, err := s.agentTasks(ctx, sessionID, agentID)
	if err != nil {
		return "", false, err
	}
	for _, t := range tasks {
		if t.State == StateRunning || t.State == StateAwaitingInput {
			return t.ID.String(), true, nil
		}
	}
	return "", false, nil
}

func (s *DBStore) NextQueuedTask(ctx context.Context, sessionID, agentID string) (string, string, bool, error) {
	tasks, err := s.agentTasks(ctx, sessionID, agentID)
	if err != nil {
		return "", "", false, err
	}
	for _, t := range tasks {
		if t.State == StateQueued {
			return t.ID.String(), t.Prompt, true, nil
		}
	}
	return "", "", false, nil
}

func (s *DBStore) TaskState(ctx context.Context, taskID string) (string, bool, error) {
	tid, err := uuid.Parse(taskID)
	if err != nil {
		return "", false, err
	}
	task, err := s.q.GetTask(ctx, tid)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return task.State, true, nil
}

func (s *DBStore) agentTasks(ctx context.Context, sessionID, agentID string) ([]db.Task, error) {
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		return nil, err
	}
	aid, err := uuid.Parse(agentID)
	if err != nil {
		return nil, err
	}
	return s.q.ListAgentTasks(ctx, db.ListAgentTasksParams{SessionID: sid, AgentID: aid})
}

// parseNullableUUID maps an empty string to a NULL pgtype.UUID and a valid
// string to a set one. Invalid non-empty strings yield NULL rather than an
// error — these fields (requested_by, anchor_message_id) are advisory.
func parseNullableUUID(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
