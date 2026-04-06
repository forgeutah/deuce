import { useState, useRef, useEffect, useCallback } from "react";
import { Loader2, AlertCircle } from "lucide-react";
import { useSessionStore } from "@/stores/session-store";

const PROMPT = "\x1b[32muser@deuce\x1b[0m:\x1b[34m~/project\x1b[0m$ ";

const MOCK_COMMANDS: Record<string, string> = {
  ls: "cmd/\ninternal/\npkg/\ngo.mod\ngo.sum\nmain.go\nREADME.md",
  "ls -la":
    "total 32\ndrwxr-xr-x  8 user user  256 May  8 14:30 .\ndrwxr-xr-x  3 user user   96 May  8 10:00 ..\n-rw-r--r--  1 user user  147 May  8 14:30 go.mod\n-rw-r--r--  1 user user 1024 May  8 14:30 go.sum\n-rw-r--r--  1 user user  523 May  8 14:30 main.go\ndrwxr-xr-x  3 user user   96 May  8 14:30 cmd\ndrwxr-xr-x  5 user user  160 May  8 14:30 internal\ndrwxr-xr-x  3 user user   96 May  8 14:30 pkg",
  pwd: "/home/user/project",
  "git status":
    "On branch feat/auth-module\nChanges not staged for commit:\n  modified:   internal/auth/validate.go\n  modified:   internal/auth/validate_test.go\n\nno changes added to commit",
  "git log --oneline -5":
    "a1b2c3d Add token expiration check\n4e5f6a7 Refactor auth middleware\n8b9c0d1 Add rate limiting\n2e3f4a5 Initial auth module\n6b7c8d9 Project setup",
  "go test ./...":
    "ok  \tforge-api/internal/auth\t0.003s\nok  \tforge-api/internal/api\t0.012s\nok  \tforge-api/pkg/middleware\t0.008s\nPASS",
  "go build ./...": "",
  whoami: "user",
  date: new Date().toString(),
  clear: "__CLEAR__",
  help: "This is a mock terminal. Available commands: ls, pwd, git status, git log, go test, go build, whoami, date, clear",
};

export function TerminalView() {
  const { activeSessionId, sessions } = useSessionStore();
  const [lines, setLines] = useState<string[]>([]);
  const [currentInput, setCurrentInput] = useState("");
  const [history, setHistory] = useState<string[]>([]);
  const [historyIndex, setHistoryIndex] = useState(-1);
  const terminalRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  const session = sessions.find((s) => s.id === activeSessionId);

  useEffect(() => {
    if (session?.workspaceStatus === "ready" && lines.length === 0) {
      setLines([
        "\x1b[33mConnected to workspace\x1b[0m",
        "",
      ]);
    }
  }, [session?.workspaceStatus, lines.length]);

  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.scrollTop = terminalRef.current.scrollHeight;
    }
  }, [lines]);

  const executeCommand = useCallback(
    (cmd: string) => {
      const trimmed = cmd.trim();
      if (!trimmed) {
        setLines((prev) => [...prev, PROMPT]);
        return;
      }

      setHistory((prev) => [...prev, trimmed]);
      setHistoryIndex(-1);

      const result = MOCK_COMMANDS[trimmed];
      if (result === "__CLEAR__") {
        setLines([]);
        return;
      }

      const output = result ?? `bash: ${trimmed.split(" ")[0]}: command simulated`;
      const newLines = [PROMPT + trimmed];
      if (output) newLines.push(output);

      setLines((prev) => [...prev, ...newLines]);
    },
    [],
  );

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") {
      executeCommand(currentInput);
      setCurrentInput("");
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      if (history.length > 0) {
        const newIndex =
          historyIndex === -1 ? history.length - 1 : Math.max(0, historyIndex - 1);
        setHistoryIndex(newIndex);
        setCurrentInput(history[newIndex]);
      }
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      if (historyIndex !== -1) {
        const newIndex = historyIndex + 1;
        if (newIndex >= history.length) {
          setHistoryIndex(-1);
          setCurrentInput("");
        } else {
          setHistoryIndex(newIndex);
          setCurrentInput(history[newIndex]);
        }
      }
    }
  };

  const focusInput = () => inputRef.current?.focus();

  if (!session) return null;

  if (session.workspaceStatus === "starting") {
    return (
      <div className="flex h-full items-center justify-center bg-background-inset">
        <div className="text-center">
          <Loader2 className="mx-auto h-8 w-8 text-warning animate-spin mb-3" />
          <p className="text-sm text-foreground-muted">Connecting to workspace...</p>
        </div>
      </div>
    );
  }

  if (session.workspaceStatus === "failed") {
    return (
      <div className="flex h-full items-center justify-center bg-background-inset">
        <div className="text-center">
          <AlertCircle className="mx-auto h-8 w-8 text-danger mb-3" />
          <p className="text-sm text-foreground-muted">Failed to connect</p>
          <button className="mt-2 text-xs text-accent hover:underline">Retry</button>
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex h-full flex-col bg-background-inset font-mono text-[13px] leading-5 text-foreground cursor-text"
      onClick={focusInput}
    >
      <div ref={terminalRef} className="flex-1 overflow-y-auto p-3">
        {lines.map((line, i) => (
          <div key={i} className="whitespace-pre-wrap" dangerouslySetInnerHTML={{
            __html: line
              .replace(/\x1b\[32m/g, '<span class="text-success">')
              .replace(/\x1b\[34m/g, '<span class="text-accent">')
              .replace(/\x1b\[33m/g, '<span class="text-warning">')
              .replace(/\x1b\[0m/g, '</span>')
          }} />
        ))}

        {/* Active prompt line */}
        <div className="flex">
          <span
            dangerouslySetInnerHTML={{
              __html: PROMPT
                .replace(/\x1b\[32m/g, '<span class="text-success">')
                .replace(/\x1b\[34m/g, '<span class="text-accent">')
                .replace(/\x1b\[0m/g, '</span>')
            }}
          />
          <input
            ref={inputRef}
            value={currentInput}
            onChange={(e) => setCurrentInput(e.target.value)}
            onKeyDown={handleKeyDown}
            className="flex-1 bg-transparent outline-none caret-accent"
            autoFocus
            spellCheck={false}
          />
        </div>
      </div>
    </div>
  );
}
