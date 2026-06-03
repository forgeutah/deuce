package workspace

import (
	"context"
	"errors"
	"testing"
)

// fakeRunnerOutput returns canned output for the docker ps call. Tests swap
// m.runner with a closure that returns one of these. The format mirrors what
// `docker ps -a --format '{{.Label "dev.containers.id"}}\t{{.State}}'` emits.
func newFakeRunner(output []byte, err error) commandRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return output, err
	}
}

func TestBulkContainerStatus_HappyPath(t *testing.T) {
	m := NewManager("", "")
	m.runner = newFakeRunner([]byte(
		"uid-aaa\trunning\n"+
			"uid-bbb\trunning\n"+
			"uid-ccc\texited\n",
	), nil)

	got, err := m.BulkContainerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]ContainerState{
		"uid-aaa": ContainerRunning,
		"uid-bbb": ContainerRunning,
		"uid-ccc": ContainerStopped,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("uid %q: got %q, want %q", k, got[k], v)
		}
	}
}

func TestBulkContainerStatus_EmptyOutput(t *testing.T) {
	m := NewManager("", "")
	m.runner = newFakeRunner([]byte(""), nil)

	got, err := m.BulkContainerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %d entries: %+v", len(got), got)
	}
}

func TestBulkContainerStatus_RunnerError(t *testing.T) {
	m := NewManager("", "")
	bang := errors.New("docker daemon not running")
	m.runner = newFakeRunner([]byte("Cannot connect to the Docker daemon"), bang)

	_, err := m.BulkContainerStatus(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, bang) {
		t.Errorf("expected wrapped runner error, got: %v", err)
	}
}

func TestBulkContainerStatus_MalformedLineSkipped(t *testing.T) {
	m := NewManager("", "")
	// Second line has no tab — should be skipped with a warn.
	m.runner = newFakeRunner([]byte(
		"uid-aaa\trunning\n"+
			"no-tab-here\n"+
			"uid-bbb\texited\n",
	), nil)

	got, err := m.BulkContainerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 entries (malformed line skipped), got %d: %+v", len(got), got)
	}
	if got["uid-aaa"] != ContainerRunning {
		t.Errorf("uid-aaa: got %q, want running", got["uid-aaa"])
	}
	if got["uid-bbb"] != ContainerStopped {
		t.Errorf("uid-bbb: got %q, want stopped", got["uid-bbb"])
	}
}

func TestBulkContainerStatus_DuplicateUidKeepsLast(t *testing.T) {
	m := NewManager("", "")
	// Same uid twice — last one wins. Real-world this should not happen
	// (one container per workspace), but a stale prior container could
	// produce it transiently.
	m.runner = newFakeRunner([]byte(
		"uid-dup\trunning\n"+
			"uid-dup\texited\n",
	), nil)

	got, err := m.BulkContainerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["uid-dup"] != ContainerStopped {
		t.Errorf("uid-dup: got %q, want stopped (last-wins)", got["uid-dup"])
	}
}

func TestBulkContainerStatus_StateVariantsCollapseToStopped(t *testing.T) {
	m := NewManager("", "")
	// Docker emits several non-running states; reconciler only cares
	// about running vs not-running.
	m.runner = newFakeRunner([]byte(
		"uid-created\tcreated\n"+
			"uid-paused\tpaused\n"+
			"uid-exited\texited\n"+
			"uid-dead\tdead\n"+
			"uid-restarting\trestarting\n"+
			"uid-running\trunning\n",
	), nil)

	got, err := m.BulkContainerStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, uid := range []string{"uid-created", "uid-paused", "uid-exited", "uid-dead", "uid-restarting"} {
		if got[uid] != ContainerStopped {
			t.Errorf("%s: expected stopped, got %q", uid, got[uid])
		}
	}
	if got["uid-running"] != ContainerRunning {
		t.Errorf("uid-running: expected running, got %q", got["uid-running"])
	}
}
