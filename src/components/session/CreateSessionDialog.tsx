import { useState, useEffect, useMemo } from "react";
import { Hash, Loader2, Lock, Globe, Search, ChevronDown } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";

interface GitHubOrg {
  login: string;
  avatarUrl: string;
}

interface GitHubRepo {
  name: string;
  fullName: string;
  cloneUrl: string;
  description: string;
  language: string;
  private: boolean;
  defaultBranch: string;
}

interface AgentPreset {
  id: string;
  name: string;
  role: string;
  color: string;
  description: string;
}

function slugify(input: string): string {
  return input
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/[\s_]+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 48);
}

function isValidSlug(slug: string): boolean {
  return /^[a-z0-9][a-z0-9-]*[a-z0-9]$/.test(slug) || /^[a-z0-9]$/.test(slug);
}

const MAX_DESCRIPTION_LENGTH = 200;

export function CreateSessionDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  // GitHub orgs
  const [orgs, setOrgs] = useState<GitHubOrg[]>([]);
  const [orgsLoading, setOrgsLoading] = useState(false);
  const [selectedOrg, setSelectedOrg] = useState<GitHubOrg | null>(null);
  const [orgDropdownOpen, setOrgDropdownOpen] = useState(false);

  // Repos for selected org
  const [repos, setRepos] = useState<GitHubRepo[]>([]);
  const [reposLoading, setReposLoading] = useState(false);
  const [reposError, setReposError] = useState<string | null>(null);
  const [selectedRepo, setSelectedRepo] = useState<GitHubRepo | null>(null);
  const [repoSearch, setRepoSearch] = useState("");

  // Agents
  const [agents, setAgents] = useState<AgentPreset[]>([]);
  const [selectedAgentIds, setSelectedAgentIds] = useState<Set<string>>(
    new Set(),
  );
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { projects, setActiveSession, addSession } = useSessionStore();

  const slug = slugify(name);
  const slugValid = slug.length > 0 && isValidSlug(slug);

  const filteredRepos = useMemo(() => {
    if (!repoSearch) return repos;
    const q = repoSearch.toLowerCase();
    return repos.filter(
      (r) =>
        r.name.toLowerCase().includes(q) ||
        r.fullName.toLowerCase().includes(q) ||
        r.description?.toLowerCase().includes(q),
    );
  }, [repos, repoSearch]);

  // Load orgs and agents when dialog opens
  useEffect(() => {
    if (!open) return;

    setOrgsLoading(true);
    api
      .listGitHubOrgs()
      .then((orgList) => {
        setOrgs(orgList);
        // Auto-select the first org (personal account)
        if (orgList.length > 0 && !selectedOrg) {
          setSelectedOrg(orgList[0]);
        }
      })
      .catch(() => {})
      .finally(() => setOrgsLoading(false));

    api.listAgents().then((agentList) => {
      setAgents(agentList);
      const coder = agentList.find((a: AgentPreset) => a.role === "coder");
      if (coder) {
        setSelectedAgentIds(new Set([coder.id]));
      }
    });
  }, [open]);

  // Load repos when org changes
  useEffect(() => {
    if (!selectedOrg) return;
    setRepos([]);
    setSelectedRepo(null);
    setRepoSearch("");
    setReposLoading(true);
    setReposError(null);

    api
      .listGitHubRepos(selectedOrg.login)
      .then(setRepos)
      .catch((err) => setReposError(err.message))
      .finally(() => setReposLoading(false));
  }, [selectedOrg]);

  const toggleAgent = (id: string) => {
    setSelectedAgentIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const handleCreate = async () => {
    if (!slugValid || !selectedRepo) return;
    setCreating(true);
    setError(null);

    try {
      const projectId = projects[0]?.id;
      if (!projectId) {
        setError("No project available");
        setCreating(false);
        return;
      }

      const session = await api.createSession({
        name: slug,
        description: description.trim(),
        projectId,
        repoUrl: selectedRepo.cloneUrl,
        agentIds: Array.from(selectedAgentIds),
        memberIds: [],
      });

      addSession(session);
      setActiveSession(session.id);
      onOpenChange(false);
      resetForm();
    } catch (err: any) {
      setError(err.message ?? "Failed to create session");
    } finally {
      setCreating(false);
    }
  };

  const resetForm = () => {
    setName("");
    setDescription("");
    setSelectedOrg(null);
    setSelectedRepo(null);
    setRepoSearch("");
    setSelectedAgentIds(new Set());
    setError(null);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="bg-background-overlay border-border text-foreground max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-foreground-emphasis">
            Create Session
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          {/* Session Name */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Session Name
            </label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="auth-module"
              className="bg-background-input border-border-muted text-foreground"
            />
            {name && (
              <div className="mt-1 flex items-center gap-1">
                <Hash className="h-3 w-3 text-foreground-subtle" />
                <span
                  className={cn(
                    "text-xs font-mono",
                    slugValid ? "text-foreground-muted" : "text-danger",
                  )}
                >
                  {slug || "invalid name"}
                </span>
              </div>
            )}
          </div>

          {/* Description */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Description <span className="text-foreground-subtle">(optional)</span>
            </label>
            <Input
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="What's this session for?"
              maxLength={MAX_DESCRIPTION_LENGTH}
              className="bg-background-input border-border-muted text-foreground"
            />
          </div>

          {/* Org Selector */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Organization
            </label>
            {orgsLoading ? (
              <div className="flex items-center gap-2 rounded-md border border-border-muted bg-background-input px-3 py-2">
                <Loader2 className="h-3.5 w-3.5 animate-spin text-foreground-subtle" />
                <span className="text-xs text-foreground-subtle">
                  Loading...
                </span>
              </div>
            ) : (
              <div className="relative">
                <button
                  onClick={() => setOrgDropdownOpen(!orgDropdownOpen)}
                  className="flex w-full items-center gap-2 rounded-md border border-border-muted bg-background-input px-3 py-2 text-left text-xs hover:border-border-emphasis"
                >
                  {selectedOrg ? (
                    <>
                      <img
                        src={selectedOrg.avatarUrl}
                        alt=""
                        className="h-4 w-4 rounded-full"
                      />
                      <span className="flex-1 text-foreground">
                        {selectedOrg.login}
                      </span>
                    </>
                  ) : (
                    <span className="flex-1 text-foreground-subtle">
                      Select an organization...
                    </span>
                  )}
                  <ChevronDown className="h-3 w-3 text-foreground-subtle" />
                </button>

                {orgDropdownOpen && (
                  <div className="absolute z-50 mt-1 w-full rounded-md border border-border bg-background-overlay shadow-lg">
                    {orgs.map((org) => (
                      <button
                        key={org.login}
                        onClick={() => {
                          setSelectedOrg(org);
                          setOrgDropdownOpen(false);
                        }}
                        className={cn(
                          "flex w-full items-center gap-2 px-3 py-2 text-left text-xs hover:bg-background-hover",
                          selectedOrg?.login === org.login &&
                            "bg-background-emphasis",
                        )}
                      >
                        <img
                          src={org.avatarUrl}
                          alt=""
                          className="h-4 w-4 rounded-full"
                        />
                        <span className="text-foreground">{org.login}</span>
                      </button>
                    ))}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Repository */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Repository
            </label>

            {!selectedOrg ? (
              <p className="text-xs text-foreground-subtle">
                Select an organization first
              </p>
            ) : reposLoading ? (
              <div className="flex items-center gap-2 rounded-md border border-border-muted bg-background-input p-3">
                <Loader2 className="h-4 w-4 animate-spin text-foreground-subtle" />
                <span className="text-xs text-foreground-subtle">
                  Loading repos...
                </span>
              </div>
            ) : reposError ? (
              <div className="rounded-md border border-danger-muted bg-danger-muted/20 p-3">
                <p className="text-xs text-danger">{reposError}</p>
                <button
                  onClick={() => {
                    if (selectedOrg) {
                      setReposLoading(true);
                      setReposError(null);
                      api
                        .listGitHubRepos(selectedOrg.login)
                        .then(setRepos)
                        .catch((e) => setReposError(e.message))
                        .finally(() => setReposLoading(false));
                    }
                  }}
                  className="mt-1 text-xs text-accent hover:underline"
                >
                  Retry
                </button>
              </div>
            ) : (
              <div className="rounded-md border border-border-muted bg-background-input">
                {/* Search */}
                <div className="relative border-b border-border-muted">
                  <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-foreground-subtle" />
                  <input
                    value={repoSearch}
                    onChange={(e) => setRepoSearch(e.target.value)}
                    placeholder="Search repos..."
                    className="w-full bg-transparent py-2 pl-7 pr-3 text-xs text-foreground placeholder:text-foreground-subtle focus:outline-none"
                  />
                </div>

                {/* Repo list */}
                <div className="h-48 overflow-y-auto">
                  <div className="flex flex-col">
                    {filteredRepos.map((repo) => (
                      <button
                        key={repo.fullName}
                        onClick={() => setSelectedRepo(repo)}
                        className={cn(
                          "flex items-center gap-2 px-3 py-2 text-left text-xs hover:bg-background-hover",
                          selectedRepo?.fullName === repo.fullName &&
                            "bg-background-emphasis",
                        )}
                      >
                        {repo.private ? (
                          <Lock className="h-3 w-3 shrink-0 text-warning" />
                        ) : (
                          <Globe className="h-3 w-3 shrink-0 text-foreground-subtle" />
                        )}
                        <span className="flex-1 truncate font-medium text-foreground">
                          {repo.name}
                        </span>
                        {repo.language && (
                          <span className="rounded-full bg-background-subtle px-1.5 py-0.5 text-[10px] text-foreground-muted">
                            {repo.language}
                          </span>
                        )}
                      </button>
                    ))}
                    {filteredRepos.length === 0 && (
                      <p className="px-3 py-4 text-center text-xs text-foreground-subtle">
                        No repos found
                      </p>
                    )}
                  </div>
                </div>
              </div>
            )}

            {selectedRepo && (
              <div className="mt-1 text-xs text-accent">
                Selected: {selectedRepo.fullName}
              </div>
            )}
          </div>

          {/* Agents */}
          <div>
            <label className="mb-1 block text-xs font-medium text-foreground-muted">
              Agents
            </label>
            <div className="flex flex-col gap-1">
              {agents.map((agent) => (
                <label
                  key={agent.id}
                  className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 hover:bg-background-hover"
                >
                  <input
                    type="checkbox"
                    checked={selectedAgentIds.has(agent.id)}
                    onChange={() => toggleAgent(agent.id)}
                    className="accent-accent"
                  />
                  <div
                    className="flex h-5 w-5 items-center justify-center rounded text-[10px] font-semibold text-foreground-on-emphasis"
                    style={{ backgroundColor: agent.color }}
                  >
                    {agent.name[0]}
                  </div>
                  <span className="text-xs font-medium text-foreground">
                    {agent.name}
                  </span>
                  <span className="text-[10px] text-foreground-subtle">
                    {agent.description}
                  </span>
                </label>
              ))}
            </div>
          </div>

          {/* Error */}
          {error && <p className="text-xs text-danger">{error}</p>}

          {/* Submit */}
          <Button
            onClick={handleCreate}
            disabled={!slugValid || !selectedRepo || creating}
            className="w-full bg-accent-emphasis text-foreground-on-emphasis hover:bg-accent"
          >
            {creating ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                Creating...
              </>
            ) : (
              "Create Session"
            )}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
