import { useState } from "react";
import { Loader2, Trash2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { api, ApiError } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";

interface Props {
  sessionId: string;
  sessionName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function DeleteWorkspaceDialog({
  sessionId,
  sessionName,
  open,
  onOpenChange,
}: Props) {
  const updateWorkspaceStatus = useSessionStore((s) => s.updateWorkspaceStatus);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleDelete() {
    if (pending) return;
    setError(null);
    setPending(true);
    try {
      const updated = await api.deleteWorkspace(sessionId);
      updateWorkspaceStatus(sessionId, updated.workspaceStatus);
      onOpenChange(false);
    } catch (err) {
      const message =
        err instanceof ApiError ? err.message : "Delete failed. Try again.";
      setError(message);
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Delete workspace for #{sessionName}?</DialogTitle>
          <DialogDescription>
            The devpod container and any in-container work will be permanently
            removed. The session, chat history, and messages are kept — you can
            rebuild later from the recovery card.
          </DialogDescription>
        </DialogHeader>

        {error && (
          <p className="text-xs text-danger" role="alert">
            {error}
          </p>
        )}

        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Cancel
          </Button>
          <Button
            type="button"
            onClick={handleDelete}
            disabled={pending}
            className="gap-2 bg-danger text-foreground-on-emphasis hover:bg-danger/90"
          >
            {pending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="h-4 w-4" />
            )}
            Delete workspace
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
