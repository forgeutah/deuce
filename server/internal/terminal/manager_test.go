package terminal

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

// fakeClient records the calls a Session makes on it, in order, so tests can
// assert on both the content and the sequencing of replay vs. live delivery.
type fakeClient struct {
	mu sync.Mutex

	live     [][]byte
	replay   [][]byte
	complete int
	calls    []string // ordered call log: "replay", "complete", "live"

	replayErr   error
	completeErr error
}

func (c *fakeClient) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.live = append(c.live, append([]byte(nil), p...))
	c.calls = append(c.calls, "live")
	return len(p), nil
}

func (c *fakeClient) WriteReplay(p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.replayErr != nil {
		return c.replayErr
	}
	c.replay = append(c.replay, append([]byte(nil), p...))
	c.calls = append(c.calls, "replay")
	return nil
}

func (c *fakeClient) ReplayComplete() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.completeErr != nil {
		return c.completeErr
	}
	c.complete++
	c.calls = append(c.calls, "complete")
	return nil
}

func (c *fakeClient) snapshot() ([][]byte, [][]byte, int, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live, c.replay, c.complete, append([]string(nil), c.calls...)
}

// newTestSession wires a Session to an os.Pipe standing in for the PTY, so
// tests exercise the real readLoop fan-out rather than a reimplementation.
// Writing to the returned *os.File simulates shell output.
func newTestSession(t *testing.T) (*Session, *os.File) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	s := &Session{
		ptmx:    r,
		clients: make(map[Client]bool),
		done:    make(chan struct{}),
	}
	go s.readLoop("test")

	t.Cleanup(func() {
		w.Close()
		<-s.done
		r.Close()
	})
	return s, w
}

// emit writes to the fake PTY and waits for readLoop to fan it out.
func emit(t *testing.T, s *Session, w *os.File, data string) {
	t.Helper()
	if _, err := w.Write([]byte(data)); err != nil {
		t.Fatalf("write to fake pty: %v", err)
	}
	waitForReplay(t, s, len(data))
}

// waitForReplay blocks until the session's replay buffer has grown to at
// least n bytes, so tests don't race the readLoop goroutine.
func waitForReplay(t *testing.T, s *Session, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		got := len(s.replay)
		s.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d replay bytes", n)
}

func TestAddClientReplaysThenMarksComplete(t *testing.T) {
	s, w := newTestSession(t)
	emit(t, s, w, "hello from the shell")

	c := &fakeClient{}
	s.AddClient(c)

	_, replay, complete, calls := c.snapshot()
	if len(replay) != 1 || !bytes.Equal(replay[0], []byte("hello from the shell")) {
		t.Errorf("replay = %q, want one chunk of the buffered output", replay)
	}
	if complete != 1 {
		t.Errorf("ReplayComplete called %d times, want 1", complete)
	}
	// Ordering is the whole point: history, then the boundary, then live.
	if len(calls) != 2 || calls[0] != "replay" || calls[1] != "complete" {
		t.Errorf("call order = %v, want [replay complete]", calls)
	}
}

func TestAddClientMarksCompleteWithEmptyReplayBuffer(t *testing.T) {
	// The first client on a fresh PTY has nothing to replay, but still needs
	// the boundary marker — otherwise it stays muted until its fallback fires.
	s, _ := newTestSession(t)

	c := &fakeClient{}
	s.AddClient(c)

	_, replay, complete, _ := c.snapshot()
	if len(replay) != 0 {
		t.Errorf("WriteReplay called with empty buffer: %q", replay)
	}
	if complete != 1 {
		t.Errorf("ReplayComplete called %d times, want 1", complete)
	}
}

func TestOutputAfterRegistrationGoesToLiveWrite(t *testing.T) {
	s, w := newTestSession(t)
	emit(t, s, w, "old")

	c := &fakeClient{}
	s.AddClient(c)
	emit(t, s, w, "new")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if live, _, _, _ := c.snapshot(); len(live) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	live, replay, _, calls := c.snapshot()
	if len(live) != 1 || !bytes.Equal(live[0], []byte("new")) {
		t.Errorf("live = %q, want one chunk %q", live, "new")
	}
	if len(replay) != 1 || !bytes.Equal(replay[0], []byte("old")) {
		t.Errorf("replay = %q, want only the pre-registration output", replay)
	}
	if len(calls) != 3 || calls[2] != "live" {
		t.Errorf("call order = %v, want live delivery last", calls)
	}
}

func TestAddClientSkipsRegistrationWhenReplayFails(t *testing.T) {
	s, w := newTestSession(t)
	emit(t, s, w, "old")

	c := &fakeClient{replayErr: errors.New("connection gone")}
	s.AddClient(c)

	if _, _, complete, _ := c.snapshot(); complete != 0 {
		t.Errorf("ReplayComplete called after replay failure")
	}
	emit(t, s, w, "new")

	if live, _, _, _ := c.snapshot(); len(live) != 0 {
		t.Errorf("live output delivered to unregistered client: %q", live)
	}
}

func TestAddClientSkipsRegistrationWhenCompleteFails(t *testing.T) {
	s, w := newTestSession(t)

	c := &fakeClient{completeErr: errors.New("connection gone")}
	s.AddClient(c)
	emit(t, s, w, "new")

	if live, _, _, _ := c.snapshot(); len(live) != 0 {
		t.Errorf("live output delivered to unregistered client: %q", live)
	}
}

func TestSecondClientReplayIncludesOutputSinceFirstAttached(t *testing.T) {
	s, w := newTestSession(t)
	emit(t, s, w, "first")

	c1 := &fakeClient{}
	s.AddClient(c1)
	emit(t, s, w, "second")

	c2 := &fakeClient{}
	s.AddClient(c2)

	_, replay, complete, _ := c2.snapshot()
	if len(replay) != 1 || !bytes.Equal(replay[0], []byte("firstsecond")) {
		t.Errorf("second client replay = %q, want the full buffer to date", replay)
	}
	if complete != 1 {
		t.Errorf("second client ReplayComplete called %d times, want 1", complete)
	}
	// The first client must not be re-replayed when a second one attaches.
	if _, r1, n1, _ := c1.snapshot(); len(r1) != 1 || n1 != 1 {
		t.Errorf("first client saw replay=%d complete=%d, want 1 and 1", len(r1), n1)
	}
}

func TestAppendReplayTrimsToMostRecentBytes(t *testing.T) {
	s := &Session{clients: make(map[Client]bool), done: make(chan struct{})}

	// Marker bytes are disjoint from the filler so counting stays unambiguous.
	const marker = "END!"
	s.appendReplay(bytes.Repeat([]byte("a"), replayBufferSize))
	s.appendReplay([]byte(marker))

	if len(s.replay) != replayBufferSize {
		t.Fatalf("replay length = %d, want %d", len(s.replay), replayBufferSize)
	}
	if !bytes.HasSuffix(s.replay, []byte(marker)) {
		t.Errorf("replay lost the most recent bytes")
	}
	// The trim must drop from the front: exactly len(marker) of the original
	// filler should be gone, not an equivalent amount from the recent end.
	if got := bytes.Count(s.replay, []byte("a")); got != replayBufferSize-len(marker) {
		t.Errorf("filler retained = %d bytes, want %d", got, replayBufferSize-len(marker))
	}
}
