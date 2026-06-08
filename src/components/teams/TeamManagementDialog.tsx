import { useEffect, useState } from "react";
import { Loader2, Plus, Users } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";
import type { TeamBrowseItem } from "@/types";

// TeamManagementDialog lets a user browse every team, join or leave teams, and
// create new ones. Team membership is the read boundary for sessions, so
// joining a team makes its sessions appear in the sidebar (and leaving removes
// them). The default team can't be left — it's a user's floor of visibility.
export function TeamManagementDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { currentUser, setTeams, setSessions } = useSessionStore();
  const [teams, setLocalTeams] = useState<TeamBrowseItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadTeams = () => {
    setLoading(true);
    setError(null);
    return api
      .listAllTeams()
      .then(setLocalTeams)
      .catch((err: unknown) =>
        setError(err instanceof Error ? err.message : "Failed to load teams"),
      )
      .finally(() => setLoading(false));
  };

  useEffect(() => {
    if (!open) return;
    // loadTeams flips loading state synchronously; that's intentional here
    // (mirrors ManageMembersDialog) and safe — it runs only when the dialog
    // opens, not on every render.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void loadTeams();
  }, [open]);

  // After a membership change, refresh the browse list plus the store's teams
  // and sessions so the sidebar regroups (team names + which sessions show).
  const refreshAll = async () => {
    await Promise.all([
      loadTeams(),
      api.listTeams().then(setTeams).catch(() => {}),
      api.listSessions().then(setSessions).catch(() => {}),
    ]);
  };

  const handleJoin = async (team: TeamBrowseItem) => {
    setBusyId(team.id);
    setError(null);
    try {
      await api.joinTeam(team.id);
      await refreshAll();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to join team");
    } finally {
      setBusyId(null);
    }
  };

  const handleLeave = async (team: TeamBrowseItem) => {
    if (!currentUser) return;
    setBusyId(team.id);
    setError(null);
    try {
      await api.leaveTeam(team.id, currentUser.id);
      await refreshAll();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to leave team");
    } finally {
      setBusyId(null);
    }
  };

  const handleCreate = async () => {
    const name = newName.trim();
    if (!name || creating) return;
    setCreating(true);
    setError(null);
    try {
      await api.createTeam(name);
      setNewName("");
      await refreshAll();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to create team");
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-background-overlay border-border text-foreground max-w-md">
        <DialogHeader>
          <DialogTitle className="text-foreground-emphasis">Teams</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          {/* Team list */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              All teams
            </label>
            <div className="flex flex-col gap-0.5">
              {loading ? (
                <div className="flex items-center gap-2 px-2 py-4">
                  <Loader2 className="h-4 w-4 animate-spin text-foreground-subtle" />
                  <span className="text-xs text-foreground-subtle">
                    Loading teams...
                  </span>
                </div>
              ) : teams.length === 0 ? (
                <p className="px-2 py-4 text-center text-xs text-foreground-subtle">
                  No teams yet.
                </p>
              ) : (
                teams.map((team) => (
                  <div
                    key={team.id}
                    className="flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-background-hover"
                  >
                    <Users className="h-4 w-4 shrink-0 text-foreground-subtle" />
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-xs font-medium text-foreground">
                        {team.name}
                        {team.isDefault && (
                          <span className="ml-1 text-[10px] text-foreground-subtle">
                            (default)
                          </span>
                        )}
                      </div>
                      <div className="text-[10px] text-foreground-subtle">
                        {team.memberCount}{" "}
                        {team.memberCount === 1 ? "member" : "members"}
                      </div>
                    </div>
                    {team.isMember ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busyId === team.id || team.isDefault}
                        title={
                          team.isDefault
                            ? "You can't leave the default team"
                            : "Leave team"
                        }
                        onClick={() => handleLeave(team)}
                        className="h-7 text-xs text-foreground-muted hover:text-danger"
                      >
                        {busyId === team.id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          "Leave"
                        )}
                      </Button>
                    ) : (
                      <Button
                        size="sm"
                        disabled={busyId === team.id}
                        onClick={() => handleJoin(team)}
                        className="h-7 text-xs"
                      >
                        {busyId === team.id ? (
                          <Loader2 className="h-3.5 w-3.5 animate-spin" />
                        ) : (
                          "Join"
                        )}
                      </Button>
                    )}
                  </div>
                ))
              )}
            </div>
          </div>

          {/* Create team */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Create a team
            </label>
            <div className="flex items-center gap-2">
              <input
                value={newName}
                onChange={(e) => setNewName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    void handleCreate();
                  }
                }}
                placeholder="Team name"
                className="flex-1 rounded-md border border-border-muted bg-background-input px-3 py-2 text-xs text-foreground placeholder:text-foreground-subtle focus:border-accent focus:outline-none"
              />
              <Button
                size="sm"
                disabled={!newName.trim() || creating}
                onClick={handleCreate}
                className="h-9 gap-1.5"
              >
                {creating ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Plus className="h-3.5 w-3.5" />
                )}
                Create
              </Button>
            </div>
          </div>

          {error && <p className="text-xs text-danger">{error}</p>}
        </div>
      </DialogContent>
    </Dialog>
  );
}
