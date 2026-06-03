import { useState } from "react";
import { AlertCircle, Loader2, Play, RefreshCw, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { api, ApiError } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";
import type { Session, WorkspaceStatus } from "@/types";

type WorkspaceAction = "start" | "rebuild";

const ACTION_LABELS: Record<WorkspaceAction, string> = {
  start: "Start workspace",
  rebuild: "Rebuild workspace",
};

const ACTION_FNS: Record<WorkspaceAction, (id: string) => Promise<Session>> = {
  start: api.startWorkspace,
  rebuild: api.rebuildWorkspace,
};

// stateMessage tells the user what state the workspace is in, in language they
// understand. The keys cover every non-live workspace_status the recovery card
// can render. ready/starting never land here — the card is hidden when those
// hold (handled by the parent).
const STATE_MESSAGES: Partial<Record<WorkspaceStatus, string>> = {
  stopped: "Workspace is stopped.",
  missing: "Workspace no longer exists.",
  failed: "Workspace is in a failed state.",
  stopping: "Stopping the workspace…",
  rebuilding: "Rebuilding the workspace…",
  deleting: "Deleting the workspace…",
};

function actionForStatus(status: WorkspaceStatus): WorkspaceAction | null {
  if (status === "stopped") return "start";
  if (status === "missing" || status === "failed") return "rebuild";
  return null;
}

export function RecoveryCard({ session }: { session: Session }) {
  const updateWorkspaceStatus = useSessionStore((s) => s.updateWorkspaceStatus);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const action = actionForStatus(session.workspaceStatus);
  const message = STATE_MESSAGES[session.workspaceStatus] ?? "Workspace is not available.";

  // Transitional states show a spinner + verb and disabled buttons. The
  // transition completes via the server's terminal-state broadcast, which
  // flips workspaceStatus and re-renders this card with action enabled.
  const isTransitional =
    session.workspaceStatus === "stopping" ||
    session.workspaceStatus === "rebuilding" ||
    session.workspaceStatus === "deleting";

  async function handleAction() {
    if (!action || pending) return;
    setError(null);
    setPending(true);
    try {
      const updated = await ACTION_FNS[action](session.id);
      updateWorkspaceStatus(session.id, updated.workspaceStatus);
    } catch (err) {
      const message =
        err instanceof ApiError ? err.message : "Action failed. Try again.";
      setError(message);
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex h-full items-center justify-center p-6">
      <div className="w-full max-w-md rounded-lg border border-border-muted bg-background-input p-6 shadow-sm">
        <div className="flex items-start gap-3">
          <div className="mt-0.5 shrink-0">
            {isTransitional ? (
              <Loader2 className="h-5 w-5 animate-spin text-warning" />
            ) : (
              <AlertCircle className="h-5 w-5 text-danger" />
            )}
          </div>
          <div className="flex-1">
            <h3 className="text-sm font-semibold text-foreground-emphasis">
              {message}
            </h3>
            {session.workspaceStatus === "missing" && (
              <p className="mt-1 text-xs text-foreground-muted">
                Rebuild to create a fresh container from your devcontainer
                configuration.
              </p>
            )}
            {session.workspaceStatus === "stopped" && (
              <p className="mt-1 text-xs text-foreground-muted">
                Start to resume the container without rebuilding.
              </p>
            )}
            {session.workspaceStatus === "failed" && (
              <p className="mt-1 text-xs text-foreground-muted">
                Open Logs to see the most recent output, or rebuild to start
                over.
              </p>
            )}
            {isTransitional && (
              <p className="mt-1 text-xs text-foreground-muted">
                This may take a few seconds. The tabs will re-enable when the
                workspace is ready.
              </p>
            )}
            {error && (
              <p className="mt-2 text-xs text-danger" role="alert">
                {error}
              </p>
            )}
          </div>
        </div>

        {action && !isTransitional && (
          <div className="mt-4 flex justify-end">
            <Button
              type="button"
              onClick={handleAction}
              disabled={pending}
              className="gap-2"
            >
              {pending ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : action === "start" ? (
                <Play className="h-4 w-4" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )}
              {ACTION_LABELS[action]}
            </Button>
          </div>
        )}

        {session.workspaceStatus === "missing" && !isTransitional && (
          <p className="mt-3 text-[11px] text-foreground-subtle">
            <Trash2 className="mr-1 inline h-3 w-3 align-text-bottom" />
            Chat history, messages, and members are preserved.
          </p>
        )}
      </div>
    </div>
  );
}
