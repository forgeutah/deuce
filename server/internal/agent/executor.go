package agent

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/forgeutah/deuce/server/internal/workspace"
)

// StreamFunc receives streaming output events from Claude Code execution.
type StreamFunc func(event StreamEvent)

// StreamEvent represents a parsed streaming output event from Claude Code.
type StreamEvent struct {
	Type    string // "text", "tool_use", "tool_result", "error"
	Content string // text content or tool description
}

// ExecuteParams holds the parameters for an agent execution.
type ExecuteParams struct {
	WorkspaceID     string
	AgentName       string
	SystemPrompt    string
	UserMessage     string
	ChatHistory     string // formatted chat history for context
	ClaudeSessionID string // for --resume, empty for fresh session
	Model           string
}

// ExecuteResult holds the result of an agent execution.
type ExecuteResult struct {
	Summary           string
	ExpandableContent []map[string]string
	ClaudeSessionID   string
	Error             string
}

// Executor manages Claude Code headless execution inside DevPod workspaces.
type Executor struct {
	workspaces *workspace.Manager
	apiKey     string
	timeout    time.Duration
}

// NewExecutor creates a new agent executor.
func NewExecutor(wm *workspace.Manager, apiKey string) *Executor {
	return &Executor{
		workspaces: wm,
		apiKey:     apiKey,
		timeout:    5 * time.Minute,
	}
}

// Execute runs Claude Code headless inside a devcontainer and returns structured results.
func (e *Executor) Execute(ctx context.Context, params ExecuteParams, streamFn StreamFunc) (*ExecuteResult, error) {
	if e.apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not configured")
	}

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Build the claude command
	cmdParts := []string{"claude", "-p"}

	// Add system prompt
	if params.SystemPrompt != "" {
		cmdParts = append(cmdParts, "--append-system-prompt", shellQuote(params.SystemPrompt))
	}

	// Add allowed tools
	cmdParts = append(cmdParts, "--allowedTools", "Bash,Read,Edit,Write")

	// Use stream-json for structured output
	cmdParts = append(cmdParts, "--output-format", "stream-json", "--verbose")

	// Resume session if available
	if params.ClaudeSessionID != "" {
		cmdParts = append(cmdParts, "--resume", params.ClaudeSessionID)
	}

	// Build the full prompt with chat history context
	prompt := params.UserMessage
	if params.ChatHistory != "" && params.ClaudeSessionID == "" {
		// Only inject full chat history on fresh sessions
		prompt = fmt.Sprintf("Recent conversation context:\n%s\n\nCurrent request:\n%s", params.ChatHistory, params.UserMessage)
	}

	// Build the full SSH command — pipe prompt via stdin. Prefix PATH with
	// $HOME/.local/bin since the native installer drops `claude` there and
	// `devpod ssh --command` runs a non-interactive shell.
	fullCommand := fmt.Sprintf("echo %s | %s%s", shellQuote(prompt), workspace.ClaudePathPrefix, strings.Join(cmdParts, " "))

	// Pass API key into the container via --set-env (not cmd.Env, which only sets host env)
	cmd := e.workspaces.ExecInWorkspace(ctx, params.WorkspaceID, fullCommand,
		fmt.Sprintf("ANTHROPIC_API_KEY=%s", e.apiKey),
	)

	slog.Info("executing agent", "agent", params.AgentName, "workspace", params.WorkspaceID)

	// Capture stdout for parsing
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude: %w", err)
	}

	// Parse streaming output
	result := &ExecuteResult{}
	parser := newOutputParser()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		events := parser.parseLine(line)
		for _, event := range events {
			if streamFn != nil {
				streamFn(event)
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		// Check if this was a resume failure
		if params.ClaudeSessionID != "" && ctx.Err() == nil {
			slog.Warn("claude execution failed with resume, retrying without", "agent", params.AgentName, "error", err)
			// Retry without resume
			retryParams := params
			retryParams.ClaudeSessionID = ""
			return e.Execute(ctx, retryParams, streamFn)
		}

		if ctx.Err() == context.Canceled {
			result.Error = "cancelled"
			return result, fmt.Errorf("agent execution cancelled")
		}
		if ctx.Err() == context.DeadlineExceeded {
			result.Error = "timeout"
			return result, fmt.Errorf("agent execution timed out after %v", e.timeout)
		}
		result.Error = err.Error()
		return result, fmt.Errorf("claude execution failed: %w", err)
	}

	// Extract results from parser
	result.Summary = parser.getSummary()
	result.ExpandableContent = parser.getExpandableContent()
	result.ClaudeSessionID = parser.getSessionID()

	slog.Info("agent execution complete", "agent", params.AgentName, "sessionID", result.ClaudeSessionID)
	return result, nil
}

// shellQuote wraps a string in single quotes for shell safety.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
