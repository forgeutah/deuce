package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	db "github.com/forgeutah/deuce/server/internal/db"
)

// agentActionResp / agentTaskResp / agentRunSnapshotResp mirror the frontend
// AgentAction / AgentTask / AgentRunSnapshot types (camelCase).
type agentActionResp struct {
	CallID  string `json:"callId"`
	Seq     int64  `json:"seq"`
	Tool    string `json:"tool"`
	Arg     string `json:"arg,omitempty"`
	Text    string `json:"text,omitempty"`
	Stat    string `json:"stat,omitempty"`
	Status  string `json:"status"`
	IsError bool   `json:"isError,omitempty"`
}

type agentTaskResp struct {
	ID              string            `json:"id"`
	SessionID       string            `json:"sessionId"`
	AgentID         string            `json:"agentId"`
	RequestedBy     string            `json:"requestedBy,omitempty"`
	AnchorMessageID string            `json:"anchorMessageId,omitempty"`
	Prompt          string            `json:"prompt"`
	State           string            `json:"state"`
	Seq                    int64             `json:"seq"`
	PendingQuestion        string            `json:"pendingQuestion,omitempty"`
	PendingQuestionKind    string            `json:"pendingQuestionKind,omitempty"`
	PendingQuestionOptions []string          `json:"pendingQuestionOptions,omitempty"`
	Reply                  string            `json:"reply,omitempty"`
	Actions                []agentActionResp `json:"actions"`
}

type agentRunSnapshotResp struct {
	Tasks     []agentTaskResp `json:"tasks"`
	LatestSeq int64           `json:"latestSeq"`
}

// AgentRunsSnapshot returns the current task + action state for a session plus
// the latest event seq, read in a single REPEATABLE READ transaction so the
// rows and latestSeq cannot tear under concurrent writes (R9, H1). Clients
// apply only live events with seq > latestSeq. Membership-gated (KTD14).
func (h *Handler) AgentRunsSnapshot(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_SESSION_ID", "invalid session ID")
		return
	}
	userID, err := uuid.Parse(getUserID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_USER", "invalid user ID")
		return
	}

	// Read gate (KTD1): the snapshot is read-only state, so it follows the
	// READ boundary (team membership), not session membership. A team member
	// viewing a session they have not joined sees the static task/action
	// cards; the LIVE AgentRunEvent stream stays session-gated at the WS join.
	if !h.requireSessionTeamMember(w, r, sessionID, userID) {
		return
	}

	tx, err := h.pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to begin snapshot")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.queries.WithTx(tx)

	tasks, err := q.ListSessionTasks(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list tasks")
		return
	}
	actions, err := q.ListSessionTaskActions(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DB_ERROR", "failed to list actions")
		return
	}

	writeJSON(w, http.StatusOK, buildSnapshot(tasks, actions))
}

// buildSnapshot groups actions under their tasks and derives latestSeq as the
// max seq among the rows actually returned — never a separate MAX() query, so
// no event is both absent from the snapshot and filtered out by the client's
// seq> cursor (H1). Pure for testability.
func buildSnapshot(tasks []db.Task, actions []db.TaskAction) agentRunSnapshotResp {
	byTask := make(map[uuid.UUID][]agentActionResp, len(tasks))
	for _, a := range actions {
		byTask[a.TaskID] = append(byTask[a.TaskID], agentActionResp{
			CallID: a.CallID, Seq: a.Seq, Tool: a.Tool, Arg: a.Arg, Text: a.Text,
			Stat: a.Stat, Status: a.Status, IsError: a.Status == "error",
		})
	}

	var latest int64
	resp := agentRunSnapshotResp{Tasks: make([]agentTaskResp, 0, len(tasks))}
	for _, t := range tasks {
		acts := byTask[t.ID]
		if acts == nil {
			acts = []agentActionResp{}
		}
		if t.Seq > latest {
			latest = t.Seq
		}
		for _, a := range acts {
			if a.Seq > latest {
				latest = a.Seq
			}
		}
		resp.Tasks = append(resp.Tasks, agentTaskResp{
			ID: t.ID.String(), SessionID: t.SessionID.String(), AgentID: t.AgentID.String(),
			RequestedBy:     uuidStr(t.RequestedBy.Bytes, t.RequestedBy.Valid),
			AnchorMessageID: uuidStr(t.AnchorMessageID.Bytes, t.AnchorMessageID.Valid),
			Prompt:          t.Prompt, State: t.State, Seq: t.Seq,
			PendingQuestion:        t.PendingQuestion,
			PendingQuestionKind:    t.PendingQuestionKind,
			PendingQuestionOptions: t.PendingQuestionOptions,
			Reply:                  t.Reply, Actions: acts,
		})
	}
	resp.LatestSeq = latest
	return resp
}

func uuidStr(b [16]byte, valid bool) string {
	if !valid {
		return ""
	}
	return uuid.UUID(b).String()
}

// RecoverStuckTasks reconciles tasks left running/awaiting_input by a crash to
// failed and clears their Pi sessions, BEFORE the scheduler starts (KTD10). It
// retries transient DB errors a few times, then returns the error so the caller
// can abort boot rather than serve with tasks the snapshot would report live
// forever. Order matters: clear pi sessions (keyed on the pre-failure states)
// before failing the tasks.
func RecoverStuckTasks(ctx context.Context, q *db.Queries) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = q.ClearStuckPiSessions(ctx); err == nil {
			if err = q.FailStuckTasks(ctx); err == nil {
				return nil
			}
		}
		time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
	}
	return err
}
