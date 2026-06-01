import { useState } from "react";
import { MoreVertical, Pause, RefreshCw, Trash2 } from "lucide-react";
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

function isBusy(status: WorkspaceStatus): boolean {
  return (
    status === "starting" ||
    status === "stopping" ||
    status === "rebuilding" ||
    status === "deleting"
  );
}

export function WorkspaceMenu({ session }: { session: Session }) {
  const updateWorkspaceStatus = useSessionStore((s) => s.updateWorkspaceStatus);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [pending, setPending] = useState<"stop" | "rebuild" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const busy = isBusy(session.workspaceStatus);
  const status = session.workspaceStatus;

  // Per the brainstorm: Stop and Rebuild fire immediately on click (no
  // confirm modal). Delete is the only destructive action that opens a
  // modal — wiping a container takes minutes to recover from.
  async function fireAction(action: "stop" | "rebuild") {
    if (pending || busy) return;
    setError(null);
    setPending(action);
    try {
      const fn = action === "stop" ? api.stopWorkspace : api.rebuildWorkspace;
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
        <DropdownMenuContent align="end" className="min-w-[180px]">
          <DropdownMenuItem
            onClick={() => fireAction("stop")}
            disabled={busy || pending !== null || status !== "ready"}
          >
            <Pause className="mr-2 h-4 w-4" />
            <div className="flex flex-col">
              <span>Stop workspace</span>
              <span className="text-[11px] text-foreground-subtle">
                Container halts; Start resumes it.
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
