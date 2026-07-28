import { useEffect, useState } from "react";
import { ArchiveRestore, Hash, Loader2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { ApiError } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

// The Archived view: a separate surface (kept out of the main sidebar) listing
// archived sessions so their history stays reachable. Each row can be opened
// for read-only viewing or restored back to the active sidebar.
export function ArchivedSessionsDialog({ open, onOpenChange }: Props) {
  const archivedSessions = useSessionStore((s) => s.archivedSessions);
  const loadArchivedSessions = useSessionStore((s) => s.loadArchivedSessions);
  const restoreSession = useSessionStore((s) => s.restoreSession);
  const viewArchivedSession = useSessionStore((s) => s.viewArchivedSession);

  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [restoringId, setRestoringId] = useState<string | null>(null);

  // Refetch the archived list each time the dialog opens so it reflects
  // anything archived/restored since it was last viewed.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    // Synchronous reset of the fetch-status flags when the dialog opens — the
    // accepted "synchronize with an external system on open" effect shape.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setError(null);
    setLoading(true);
    loadArchivedSessions()
      .catch((err) => {
        if (cancelled) return;
        setError(
          err instanceof ApiError
            ? err.message
            : "Failed to load archived sessions.",
        );
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, loadArchivedSessions]);

  function handleOpen(sessionId: string) {
    const session = archivedSessions.find((s) => s.id === sessionId);
    if (!session) return;
    viewArchivedSession(session);
    onOpenChange(false);
  }

  async function handleRestore(sessionId: string) {
    if (restoringId) return;
    setError(null);
    setRestoringId(sessionId);
    try {
      await restoreSession(sessionId);
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Restore failed. Try again.",
      );
    } finally {
      setRestoringId(null);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Archived sessions</DialogTitle>
          <DialogDescription>
            History is preserved for archived sessions. Open one to read it, or
            restore it to bring it back to your sidebar.
          </DialogDescription>
        </DialogHeader>

        {error && (
          <p className="text-xs text-danger" role="alert">
            {error}
          </p>
        )}

        {loading ? (
          <div className="flex items-center justify-center py-8 text-foreground-subtle">
            <Loader2 className="h-4 w-4 animate-spin" />
          </div>
        ) : archivedSessions.length === 0 ? (
          <p className="py-8 text-center text-xs text-foreground-subtle">
            No archived sessions.
          </p>
        ) : (
          <ScrollArea className="max-h-80">
            <div className="flex flex-col gap-0.5 pr-2">
              {archivedSessions.map((session) => (
                <div
                  key={session.id}
                  className="group flex items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-background-hover"
                >
                  <button
                    type="button"
                    onClick={() => handleOpen(session.id)}
                    className="flex min-w-0 flex-1 items-center gap-2 text-left"
                  >
                    <Hash className="h-4 w-4 shrink-0 text-foreground-subtle" />
                    <span className="truncate font-medium text-foreground">
                      {session.name}
                    </span>
                    {session.description && (
                      <span className="truncate text-xs text-foreground-subtle">
                        {session.description}
                      </span>
                    )}
                  </button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    className="h-7 shrink-0 gap-1.5 text-xs text-foreground-muted hover:text-foreground"
                    onClick={() => handleRestore(session.id)}
                    disabled={restoringId === session.id}
                  >
                    {restoringId === session.id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <ArchiveRestore className="h-3.5 w-3.5" />
                    )}
                    Restore
                  </Button>
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  );
}
