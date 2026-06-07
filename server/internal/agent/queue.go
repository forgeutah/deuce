package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	queueBufferSize   = 10
	workerIdleTimeout = 10 * time.Minute
)

// Task represents an agent execution request.
type Task struct {
	SessionID string
	AgentID   string
	AgentName string
	Prompt    string
	Callback  func(task Task) // Called by the worker to execute the task
}

// Queue manages per-session sequential agent execution.
type Queue struct {
	mu       sync.Mutex
	sessions map[string]chan Task
	cancels  map[string]context.CancelFunc // active execution cancel functions
	wg       sync.WaitGroup
}

// NewQueue creates a new agent execution queue.
func NewQueue() *Queue {
	return &Queue{
		sessions: make(map[string]chan Task),
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Enqueue adds a task to the session's queue. Returns an error if the queue is full.
func (q *Queue) Enqueue(task Task) error {
	q.mu.Lock()
	ch, exists := q.sessions[task.SessionID]
	if !exists {
		ch = make(chan Task, queueBufferSize)
		q.sessions[task.SessionID] = ch
		q.wg.Add(1)
		go q.worker(task.SessionID, ch)
	}
	q.mu.Unlock()

	select {
	case ch <- task:
		return nil
	default:
		return ErrQueueFull
	}
}

// SetCancel stores a cancel function for the currently executing task in a session.
func (q *Queue) SetCancel(sessionID string, cancel context.CancelFunc) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancels[sessionID] = cancel
}

// ClearCancel removes the cancel function for a session.
func (q *Queue) ClearCancel(sessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.cancels, sessionID)
}

// Cancel cancels the currently executing task in a session.
func (q *Queue) Cancel(sessionID string) bool {
	q.mu.Lock()
	cancel, ok := q.cancels[sessionID]
	q.mu.Unlock()
	if ok {
		cancel()
		return true
	}
	return false
}

// CancelSession cancels the running task and drains the queue for a session.
func (q *Queue) CancelSession(sessionID string) {
	q.Cancel(sessionID)

	q.mu.Lock()
	ch, exists := q.sessions[sessionID]
	q.mu.Unlock()

	if exists {
		// Drain remaining tasks
		for {
			select {
			case <-ch:
			default:
				return
			}
		}
	}
}

// Shutdown cancels all active executions and waits for workers to exit.
func (q *Queue) Shutdown(ctx context.Context) {
	q.mu.Lock()
	// Cancel all active executions
	for _, cancel := range q.cancels {
		cancel()
	}
	// Close all channels to signal workers to exit
	for id, ch := range q.sessions {
		close(ch)
		delete(q.sessions, id)
	}
	q.mu.Unlock()

	// Wait for workers with timeout
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("agent queue shutdown timed out")
	}
}

func (q *Queue) worker(sessionID string, ch chan Task) {
	defer q.wg.Done()
	slog.Info("agent queue worker started", "session", sessionID)

	for {
		select {
		case task, ok := <-ch:
			if !ok {
				slog.Info("agent queue worker exiting (channel closed)", "session", sessionID)
				return
			}
			task.Callback(task)

		case <-time.After(workerIdleTimeout):
			q.mu.Lock()
			// Check if the channel is still empty before cleaning up
			if len(ch) == 0 {
				delete(q.sessions, sessionID)
				q.mu.Unlock()
				slog.Info("agent queue worker exiting (idle timeout)", "session", sessionID)
				return
			}
			q.mu.Unlock()
		}
	}
}

// ErrQueueFull is returned when the agent queue for a session is full.
var ErrQueueFull = &queueFullError{}

type queueFullError struct{}

func (e *queueFullError) Error() string {
	return "agent queue is full, please wait for current work to complete"
}
