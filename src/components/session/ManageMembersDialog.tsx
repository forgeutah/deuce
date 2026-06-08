import { useEffect, useMemo, useState } from "react";
import { Loader2, Search, UserMinus, UserPlus } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";
import type { Session, User } from "@/types";

// ManageMembersDialog lets any session member add or remove people on an
// existing session. Membership is the single visibility gate, so this is how a
// teammate gains access after the session was created. The backend also
// broadcasts session_update (refreshing every client), but we apply the
// returned session optimistically so the panel updates without waiting on WS.
export function ManageMembersDialog({
  session,
  open,
  onOpenChange,
}: {
  session: Session;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { currentUser, setSessions } = useSessionStore();
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(false);
  const [search, setSearch] = useState("");
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true);
    setError(null);
    api
      .listUsers()
      .then(setUsers)
      .catch((err: unknown) =>
        setError(err instanceof Error ? err.message : "Failed to load users"),
      )
      .finally(() => setLoading(false));
  }, [open]);

  const memberIds = useMemo(
    () => new Set(session.members.map((m) => m.id)),
    [session.members],
  );

  const addableUsers = useMemo(() => {
    const q = search.trim().toLowerCase();
    return users
      .filter((u) => !memberIds.has(u.id))
      .filter(
        (u) =>
          !q ||
          u.name.toLowerCase().includes(q) ||
          u.email.toLowerCase().includes(q),
      );
  }, [users, memberIds, search]);

  // Replace the session in the store with the server's authoritative copy.
  const applyUpdatedSession = (updated: Session) => {
    const store = useSessionStore.getState();
    setSessions(
      store.sessions.map((s) => (s.id === updated.id ? updated : s)),
    );
  };

  const handleAdd = async (user: User) => {
    setBusyId(user.id);
    setError(null);
    try {
      const updated = await api.addSessionMember(session.id, {
        userId: user.id,
      });
      applyUpdatedSession(updated);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to add member");
    } finally {
      setBusyId(null);
    }
  };

  const handleRemove = async (user: User) => {
    setBusyId(user.id);
    setError(null);
    try {
      const updated = await api.removeSessionMember(session.id, user.id);
      applyUpdatedSession(updated);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to remove member");
    } finally {
      setBusyId(null);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-background-overlay border-border text-foreground max-w-md">
        <DialogHeader>
          <DialogTitle className="text-foreground-emphasis">
            Manage Members
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          {/* Current members */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Members ({session.members.length})
            </label>
            <div className="flex flex-col gap-0.5">
              {session.members.map((user) => (
                <div
                  key={user.id}
                  className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-background-hover"
                >
                  <img
                    src={user.avatar}
                    alt=""
                    className="h-6 w-6 rounded-full"
                  />
                  <div className="flex-1 min-w-0">
                    <div className="text-xs font-medium text-foreground truncate">
                      {user.name || user.email}
                      {currentUser?.id === user.id && (
                        <span className="ml-1 text-[10px] text-foreground-subtle">
                          (you)
                        </span>
                      )}
                    </div>
                    <div className="text-[10px] text-foreground-subtle truncate">
                      {user.email}
                    </div>
                  </div>
                  <button
                    onClick={() => handleRemove(user)}
                    disabled={busyId === user.id}
                    title={
                      currentUser?.id === user.id
                        ? "Leave session"
                        : "Remove from session"
                    }
                    className="flex h-6 w-6 items-center justify-center rounded text-foreground-subtle hover:bg-danger-muted/30 hover:text-danger disabled:opacity-50"
                  >
                    {busyId === user.id ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <UserMinus className="h-3.5 w-3.5" />
                    )}
                  </button>
                </div>
              ))}
            </div>
          </div>

          {/* Add people */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Add people
            </label>
            <div className="rounded-md border border-border-muted bg-background-input">
              <div className="relative border-b border-border-muted">
                <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-foreground-subtle" />
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search by name or email..."
                  className="w-full bg-transparent py-2 pl-7 pr-3 text-xs text-foreground placeholder:text-foreground-subtle focus:outline-none"
                />
              </div>
              <div className="max-h-48 overflow-y-auto">
                {loading ? (
                  <div className="flex items-center gap-2 px-3 py-4">
                    <Loader2 className="h-4 w-4 animate-spin text-foreground-subtle" />
                    <span className="text-xs text-foreground-subtle">
                      Loading users...
                    </span>
                  </div>
                ) : addableUsers.length === 0 ? (
                  <p className="px-3 py-4 text-center text-xs text-foreground-subtle">
                    {users.length <= session.members.length
                      ? "Everyone who has signed in is already a member."
                      : "No matching people."}
                  </p>
                ) : (
                  addableUsers.map((user) => (
                    <button
                      key={user.id}
                      onClick={() => handleAdd(user)}
                      disabled={busyId === user.id}
                      className={cn(
                        "flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-background-hover disabled:opacity-50",
                      )}
                    >
                      <img
                        src={user.avatar}
                        alt=""
                        className="h-6 w-6 rounded-full"
                      />
                      <div className="flex-1 min-w-0">
                        <div className="text-xs font-medium text-foreground truncate">
                          {user.name || user.email}
                        </div>
                        <div className="text-[10px] text-foreground-subtle truncate">
                          {user.email}
                        </div>
                      </div>
                      {busyId === user.id ? (
                        <Loader2 className="h-3.5 w-3.5 animate-spin text-foreground-subtle" />
                      ) : (
                        <UserPlus className="h-3.5 w-3.5 text-foreground-subtle" />
                      )}
                    </button>
                  ))
                )}
              </div>
            </div>
          </div>

          {error && <p className="text-xs text-danger">{error}</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}
