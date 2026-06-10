package handler

import (
	"testing"

	"github.com/google/uuid"

	db "github.com/forgeutah/deuce/server/internal/db"
)

func TestBuildSnapshotGroupsActionsAndDerivesLatestSeq(t *testing.T) {
	t1 := uuid.New()
	t2 := uuid.New()
	sid := uuid.New()

	tasks := []db.Task{
		{ID: t1, SessionID: sid, State: "done", Seq: 3, Reply: "ok"},
		{ID: t2, SessionID: sid, State: "awaiting_input", Seq: 7, PendingQuestion: "which?"},
	}
	actions := []db.TaskAction{
		{TaskID: t1, CallID: "c1", Seq: 2, Tool: "Bash", Status: "completed"},
		{TaskID: t1, CallID: "c2", Seq: 5, Tool: "Read", Status: "error"},
		{TaskID: t2, CallID: "c3", Seq: 6, Tool: "Bash", Status: "started"},
	}

	snap := buildSnapshot(tasks, actions)

	// latestSeq is the max across all returned task + action rows (here 7).
	if snap.LatestSeq != 7 {
		t.Errorf("latestSeq = %d, want 7", snap.LatestSeq)
	}
	if len(snap.Tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(snap.Tasks))
	}
	// t1 has its two actions; the errored one carries isError.
	var task1 agentTaskResp
	for _, tk := range snap.Tasks {
		if tk.ID == t1.String() {
			task1 = tk
		}
	}
	if len(task1.Actions) != 2 {
		t.Fatalf("t1 actions = %d, want 2", len(task1.Actions))
	}
	foundErr := false
	for _, a := range task1.Actions {
		if a.CallID == "c2" {
			if !a.IsError {
				t.Errorf("action c2 should be isError")
			}
			foundErr = true
		}
	}
	if !foundErr {
		t.Error("errored action not found")
	}
}

func TestBuildSnapshotEmpty(t *testing.T) {
	snap := buildSnapshot(nil, nil)
	if snap.LatestSeq != 0 || len(snap.Tasks) != 0 {
		t.Errorf("empty snapshot = %+v, want latestSeq 0 and no tasks", snap)
	}
	// Tasks must be a non-nil slice so it serializes as [] not null.
	if snap.Tasks == nil {
		t.Error("Tasks should be an empty slice, not nil")
	}
}

func TestUUIDStr(t *testing.T) {
	id := uuid.New()
	if got := uuidStr(id, true); got != id.String() {
		t.Errorf("uuidStr(valid) = %q, want %q", got, id.String())
	}
	if got := uuidStr(id, false); got != "" {
		t.Errorf("uuidStr(invalid) = %q, want empty", got)
	}
}
