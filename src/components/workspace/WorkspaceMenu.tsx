import { useState } from "react";
import { MoreVertical, Pause, Play, RefreshCw, Trash2 } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { api, ApiError } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";
import type { Session, WorkspaceStatus } from "@/types";
import { DeleteWorkspaceDialog } from "./DeleteWorkspaceDialog";

type PowerAction = "start" | "stop";

function isBusy(status: WorkspaceStatus): boolean {
  return (
    status === "starting" ||
    status === "stopping" ||
    status === "rebuilding" ||
    status === "deleting"
  );
}

// powerActionFor reads the current workspace state and picks which power
// action makes sense as the menu's first item. ready → Stop (running, so
// halt it). stopped → Start (halted, so resume it). Anything else returns
// undefined and the menu still renders the slot but disables it — the
// label defaults to Stop so the slot's position doesn't reflow.
function powerActionFor(status: WorkspaceStatus): PowerAction | undefined {
  if (status === "ready") return "stop";
  if (status === "stopped") return "start";
  return undefined;
}

export function WorkspaceMenu({ session }: { session: Session }) {
  const updateWorkspaceStatus = useSessionStore((s) => s.updateWorkspaceStatus);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [pending, setPending] = useState<PowerAction | "rebuild" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const busy = isBusy(session.workspaceStatus);
  const status = session.workspaceStatus;
  const powerAction = powerActionFor(status);

  // Per the brainstorm: Start/Stop/Rebuild fire immediately on click (no
  // confirm modal). Delete is the only destructive action that opens a
  // modal — wiping a container takes minutes to recover from.
  async function fireAction(action: PowerAction | "rebuild") {
    if (pending || busy) return;
    setError(null);
    setPending(action);
    try {
      const fn =
        action === "start"
          ? api.startWorkspace
          : action === "stop"
            ? api.stopWorkspace
            : api.rebuildWorkspace;
      const updated = await fn(session.id);
      updateWorkspaceStatus(session.id, updated.workspaceStatus);
    } catch (err) {
      const message =
        err instanceof ApiError ? err.message : `${action} failed. Try again.`;
      setError(message);
    } finally {
      setPending(null);
    }
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="h-7 w-7 text-foreground-muted hover:text-foreground"
            aria-label="Workspace actions"
            title="Workspace actions"
          >
            <MoreVertical className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="end"
          className="z-60 min-w-45 border-border bg-background-overlay text-foreground shadow-lg"
        >
          <DropdownMenuItem
            onClick={() => powerAction && fireAction(powerAction)}
            disabled={busy || pending !== null || powerAction === undefined}
          >
            {powerAction === "start" ? (
              <Play className="mr-2 h-4 w-4" />
            ) : (
              <Pause className="mr-2 h-4 w-4" />
            )}
            <div className="flex flex-col">
              <span>
                {powerAction === "start" ? "Start workspace" : "Stop workspace"}
              </span>
              <span className="text-[11px] text-foreground-subtle">
                {powerAction === "start"
                  ? "Resume the existing container."
                  : "Container halts; Start resumes it."}
              </span>
            </div>
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={() => fireAction("rebuild")}
            disabled={
              busy ||
              pending !== null ||
              !(status === "ready" || status === "stopped" || status === "failed")
            }
          >
            <RefreshCw className="mr-2 h-4 w-4" />
            <div className="flex flex-col">
              <span>Rebuild workspace</span>
              <span className="text-[11px] text-foreground-subtle">
                Wipes container. Uncommitted work is lost.
              </span>
            </div>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onClick={() => setDeleteOpen(true)}
            disabled={busy || pending !== null}
            className="text-danger focus:bg-danger/10"
          >
            <Trash2 className="mr-2 h-4 w-4" />
            Delete workspace…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <DeleteWorkspaceDialog
        sessionId={session.id}
        sessionName={session.name}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
      />

      {error && (
        <span className="text-xs text-danger" role="alert">
          {error}
        </span>
      )}
    </>
  );
}
