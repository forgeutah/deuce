import { useEffect, useRef } from "react";
import { ScrollText, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useSessionStore } from "@/stores/session-store";

export function LogsView() {
  const { activeSessionId, workspaceLogs, setShowLogs } = useSessionStore();
  const scrollRef = useRef<HTMLDivElement>(null);

  const logs = activeSessionId ? (workspaceLogs[activeSessionId] ?? []) : [];

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs.length]);

  return (
    <div className="flex h-full flex-col bg-background-inset">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border-muted px-3 py-1.5">
        <div className="flex items-center gap-1.5">
          <ScrollText className="h-3.5 w-3.5 text-foreground-subtle" />
          <span className="text-xs font-medium text-foreground-muted">
            Workspace Logs
          </span>
          {logs.length > 0 && (
            <span className="text-[10px] text-foreground-subtle">
              ({logs.length} lines)
            </span>
          )}
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6 text-foreground-subtle hover:text-foreground"
          onClick={() => setShowLogs(false)}
        >
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      {/* Log content */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto p-3">
        {logs.length === 0 ? (
          <div className="flex h-full items-center justify-center">
            <p className="text-xs text-foreground-subtle">
              No workspace logs yet
            </p>
          </div>
        ) : (
          <pre className="font-mono text-[12px] leading-5 text-foreground-muted">
            {logs.map((line, i) => (
              <div
                key={i}
                className={
                  line.startsWith("ERROR")
                    ? "text-danger"
                    : line.startsWith("Step") || line.startsWith("==>")
                      ? "text-foreground"
                      : undefined
                }
              >
                {line}
              </div>
            ))}
          </pre>
        )}
      </div>
    </div>
  );
}
