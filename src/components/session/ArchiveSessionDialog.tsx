import { useState } from "react";
import { Archive, Loader2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ApiError } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";

interface Props {
  sessionId: string;
  sessionName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// Confirm dialog for archiving a session. Archiving hides the session from the
// sidebar and tears down its devpod container to reclaim resources; all chat
// history is preserved and the session can be restored from the Archived view.
export function ArchiveSessionDialog({
  sessionId,
  sessionName,
  open,
  onOpenChange,
}: Props) {
  const archiveSession = useSessionStore((s) => s.archiveSession);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleArchive() {
    if (pending) return;
    setError(null);
    setPending(true);
    try {
      await archiveSession(sessionId);
      onOpenChange(false);
    } catch (err) {
      const message =
        err instanceof ApiError ? err.message : "Archive failed. Try again.";
      setError(message);
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Archive #{sessionName}?</DialogTitle>
          <DialogDescription>
            The session leaves your sidebar and its devpod container is torn
            down to free resources. All chat history and the plan are kept — you
            can reopen or restore it any time from the Archived view.
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
            onClick={handleArchive}
            disabled={pending}
            className="gap-2"
          >
            {pending ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Archive className="h-4 w-4" />
            )}
            Archive session
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
