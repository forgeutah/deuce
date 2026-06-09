package pirun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeHandle is an in-memory Pi process: commands the supervisor writes are
// decoded and recorded (auto-replying to get_state so the handshake completes),
// and the test pushes events / simulates exit via the stdout pipe.
type fakeHandle struct {
	inR, outR *io.PipeReader
	inW, outW *io.PipeWriter

	cmds     chan map[string]any
	stopped  chan struct{}
	stopOnce sync.Once
}

func newFakeHandle() *fakeHandle {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	f := &fakeHandle{inR: inR, inW: inW, outR: outR, outW: outW,
		cmds: make(chan map[string]any, 32), stopped: make(chan struct{})}
	go f.readLoop()
	return f
}

func (f *fakeHandle) readLoop() {
	sc := bufio.NewScanner(f.inR)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		if m["type"] == "get_state" {
			_, _ = f.outW.Write([]byte(`{"type":"response","command":"get_state","success":true}` + "\n"))
		}
		select {
		case f.cmds <- m:
		default:
		}
	}
}

func (f *fakeHandle) Stdin() io.WriteCloser { return f.inW }
func (f *fakeHandle) Stdout() io.Reader     { return f.outR }
func (f *fakeHandle) Wait() error           { return nil }

func (f *fakeHandle) Stop(context.Context) error {
	f.stopOnce.Do(func() {
		close(f.stopped)
		_ = f.outW.Close() // EOF → pump exits
		_ = f.inW.Close()
	})
	return nil
}

func (f *fakeHandle) push(line string) { _, _ = f.outW.Write([]byte(line + "\n")) }

// waitCmd blocks for the next command of a given type the supervisor sent.
func (f *fakeHandle) waitCmd(t *testing.T, typ string) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case m := <-f.cmds:
			if m["type"] == typ {
				return m
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q command", typ)
		}
	}
}

type fakeLauncher struct {
	mu       sync.Mutex
	handles  []*fakeHandle
	launches int
	failNext error
}

func (l *fakeLauncher) Launch(context.Context, string, []string, string) (Handle, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failNext != nil {
		err := l.failNext
		l.failNext = nil
		return nil, err
	}
	h := newFakeHandle()
	l.handles = append(l.handles, h)
	l.launches++
	return h, nil
}

func (l *fakeLauncher) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.launches
}

func waitEvent(t *testing.T, p *Process, d time.Duration) (Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-p.Events():
		return ev, ok
	case <-time.After(d):
		t.Fatal("timed out waiting for event")
		return Event{}, false
	}
}

func TestEnsureLaunchesAndHandshakes(t *testing.T) {
	l := &fakeLauncher{}
	s := NewSupervisor(l, "sk-test")
	key := Key{SessionID: "s1", AgentID: "a1"}

	p, err := s.Ensure(context.Background(), key, "ws1", "", "")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if l.count() != 1 {
		t.Errorf("launches = %d, want 1", l.count())
	}
	// The handshake must have sent get_state.
	l.handles[0].waitCmd(t, "get_state")

	if got, ok := s.Get(key); !ok || got != p {
		t.Errorf("Get did not return the live process")
	}
	_ = s.Shutdown(context.Background())
}

func TestEnsureReusesAndIsolatesKeys(t *testing.T) {
	l := &fakeLauncher{}
	s := NewSupervisor(l, "")
	ctx := context.Background()

	k1 := Key{SessionID: "s1", AgentID: "a1"}
	k2 := Key{SessionID: "s1", AgentID: "a2"}

	p1, _ := s.Ensure(ctx, k1, "ws", "", "")
	p1b, _ := s.Ensure(ctx, k1, "ws", "", "") // reuse
	p2, _ := s.Ensure(ctx, k2, "ws", "", "")  // distinct agent

	if p1 != p1b {
		t.Error("same key should reuse the same process")
	}
	if p1 == p2 {
		t.Error("distinct keys should get distinct processes")
	}
	if l.count() != 2 {
		t.Errorf("launches = %d, want 2", l.count())
	}
	_ = s.Shutdown(ctx)
}

func TestEnsureLaunchError(t *testing.T) {
	l := &fakeLauncher{failNext: errors.New("container unreachable")}
	s := NewSupervisor(l, "")
	key := Key{SessionID: "s1", AgentID: "a1"}

	if _, err := s.Ensure(context.Background(), key, "ws", "", ""); err == nil {
		t.Fatal("expected launch error")
	}
	if _, ok := s.Get(key); ok {
		t.Error("failed launch should leave no registry entry")
	}
}

func TestProcessExitEmitsSignal(t *testing.T) {
	l := &fakeLauncher{}
	s := NewSupervisor(l, "")
	key := Key{SessionID: "s1", AgentID: "a1"}

	p, err := s.Ensure(context.Background(), key, "ws", "", "")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	h := l.handles[0]

	// A run event flows through to the consumer.
	h.push(`{"type":"agent_start"}`)
	if ev, ok := waitEvent(t, p, 2*time.Second); !ok || ev.Kind != KindRunStarted {
		t.Fatalf("event = %+v ok=%v, want run_started", ev, ok)
	}

	// Process dies → events channel closes, Exit emitted, registry cleared.
	_ = h.Stop(context.Background())

	select {
	case ex := <-s.Exits():
		if ex.Key != key {
			t.Errorf("exit key = %v, want %v", ex.Key, key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no exit signal emitted")
	}
	if _, ok := s.Get(key); ok {
		t.Error("registry should be cleared after exit")
	}
	if _, ok := <-p.Events(); ok {
		t.Error("events channel should be closed after exit")
	}
}

func TestReattachSendsSwitchSession(t *testing.T) {
	l := &fakeLauncher{}
	s := NewSupervisor(l, "")
	key := Key{SessionID: "s1", AgentID: "a1"}

	if _, err := s.Ensure(context.Background(), key, "ws", "/sessions/prior.jsonl", ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	m := l.handles[0].waitCmd(t, "switch_session")
	if m["sessionPath"] != "/sessions/prior.jsonl" {
		t.Errorf("switch_session path = %v, want /sessions/prior.jsonl", m["sessionPath"])
	}
	_ = s.Shutdown(context.Background())
}

func TestSendUnknownKey(t *testing.T) {
	s := NewSupervisor(&fakeLauncher{}, "")
	if err := s.Send(Key{SessionID: "x", AgentID: "y"}, Abort{}); !errors.Is(err, ErrNoProcess) {
		t.Errorf("Send to unknown key = %v, want ErrNoProcess", err)
	}
}

func TestShutdownStopsAllAndRejectsEnsure(t *testing.T) {
	l := &fakeLauncher{}
	s := NewSupervisor(l, "")
	ctx := context.Background()
	_, _ = s.Ensure(ctx, Key{SessionID: "s", AgentID: "a"}, "ws", "", "")
	_, _ = s.Ensure(ctx, Key{SessionID: "s", AgentID: "b"}, "ws", "", "")

	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	for _, h := range l.handles {
		select {
		case <-h.stopped:
		default:
			t.Error("handle was not stopped on shutdown")
		}
	}
	if _, err := s.Ensure(ctx, Key{SessionID: "s", AgentID: "c"}, "ws", "", ""); !errors.Is(err, ErrSupervisorClosed) {
		t.Errorf("Ensure after shutdown = %v, want ErrSupervisorClosed", err)
	}
}

func TestIdleReap(t *testing.T) {
	l := &fakeLauncher{}
	s := NewSupervisor(l, "")
	s.idleTimeout = 60 * time.Millisecond // shorten for the test (same package)
	key := Key{SessionID: "s1", AgentID: "a1"}

	if _, err := s.Ensure(context.Background(), key, "ws", "", ""); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	// No activity → reaped after the idle timeout.
	select {
	case ex := <-s.Exits():
		if ex.Key != key {
			t.Errorf("reaped key = %v, want %v", ex.Key, key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle process was not reaped")
	}
	if _, ok := s.Get(key); ok {
		t.Error("reaped process should be removed from registry")
	}
}
