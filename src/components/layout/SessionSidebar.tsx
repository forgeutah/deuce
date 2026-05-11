import { useState } from "react";
import {
  Hash,
  Plus,
  Search,
  Settings,
  Users,
  ChevronDown,
  ChevronRight,
} from "lucide-react";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { useSessionStore } from "@/stores/session-store";
import { CreateSessionDialog } from "@/components/session/CreateSessionDialog";
import { AgentSettingsDialog } from "@/components/settings/AgentSettingsDialog";
import type { Session, Project } from "@/types";

function SessionCard({
  session,
  isActive,
  onClick,
}: {
  session: Session;
  isActive: boolean;
  onClick: () => void;
}) {
  const statusDot = {
    ready: "bg-success",
    starting: "bg-warning animate-pulse-dot",
    failed: "bg-danger",
    suspended: "bg-neutral-7",
  }[session.workspaceStatus];

  return (
    <button
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
        isActive
          ? "bg-background-emphasis border-l-2 border-accent text-foreground-emphasis"
          : "text-foreground hover:bg-background-hover border-l-2 border-transparent",
        session.status === "paused" && "opacity-60",
        session.status === "archived" && "opacity-40",
      )}
    >
      <Hash className="h-4 w-4 shrink-0 text-foreground-subtle" />
      <span className="truncate font-medium">{session.name}</span>
      <span className={cn("ml-auto h-2 w-2 shrink-0 rounded-full", statusDot)} />
      {session.unreadCount > 0 && (
        <span className="ml-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-medium text-foreground-on-emphasis">
          {session.unreadCount}
        </span>
      )}
    </button>
  );
}

function ProjectGroup({
  project,
  sessions,
  activeSessionId,
  onSelectSession,
}: {
  project: Project;
  sessions: Session[];
  activeSessionId: string | null;
  onSelectSession: (id: string) => void;
}) {
  const [isOpen, setIsOpen] = useState(true);

  return (
    <div className="mb-1">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex w-full items-center gap-1 px-2 py-1 text-xs font-semibold uppercase tracking-wider text-foreground-subtle hover:text-foreground-muted"
      >
        {isOpen ? (
          <ChevronDown className="h-3 w-3" />
        ) : (
          <ChevronRight className="h-3 w-3" />
        )}
        {project.name}
      </button>
      {isOpen && (
        <div className="flex flex-col gap-0.5 pl-1">
          {sessions
            .sort(
              (a, b) =>
                new Date(b.lastActivityAt).getTime() -
                new Date(a.lastActivityAt).getTime(),
            )
            .map((session) => (
              <SessionCard
                key={session.id}
                session={session}
                isActive={session.id === activeSessionId}
                onClick={() => onSelectSession(session.id)}
              />
            ))}
        </div>
      )}
    </div>
  );
}

export function SessionSidebar() {
  const [createOpen, setCreateOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const {
    projects,
    sessions,
    activeSessionId,
    searchQuery,
    setActiveSession,
    setSearchQuery,
  } = useSessionStore();

  const filteredSessions = sessions.filter((s) =>
    s.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  const sessionsByProject = projects.map((project) => ({
    project,
    sessions: filteredSessions.filter((s) => s.projectId === project.id),
  }));

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between p-3 pb-2">
        <h1 className="text-lg font-semibold text-foreground-emphasis">
          Deuce
        </h1>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-foreground-muted hover:text-foreground"
              onClick={() => setCreateOpen(true)}
            >
              <Plus className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>New Session</TooltipContent>
        </Tooltip>
      </div>

      {/* Search */}
      <div className="px-3 pb-2">
        <div className="relative">
          <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-foreground-subtle" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search sessions..."
            className="h-7 bg-background-input pl-7 text-xs border-border-muted focus:border-accent"
          />
        </div>
      </div>

      <Separator className="bg-border-muted" />

      {/* Session List */}
      <ScrollArea className="flex-1 px-2 py-2">
        {sessionsByProject.map(
          ({ project, sessions: projectSessions }) =>
            projectSessions.length > 0 && (
              <ProjectGroup
                key={project.id}
                project={project}
                sessions={projectSessions}
                activeSessionId={activeSessionId}
                onSelectSession={setActiveSession}
              />
            ),
        )}
        {filteredSessions.length === 0 && (
          <p className="px-2 py-4 text-center text-xs text-foreground-subtle">
            {searchQuery ? "No sessions match your search" : "No sessions yet"}
          </p>
        )}
      </ScrollArea>

      <Separator className="bg-border-muted" />

      {/* Footer Nav */}
      <div className="flex flex-col gap-0.5 p-2">
        <button className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-foreground-muted hover:bg-background-hover hover:text-foreground">
          <Users className="h-4 w-4" />
          Teams
        </button>
        <button
          onClick={() => setSettingsOpen(true)}
          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-foreground-muted hover:bg-background-hover hover:text-foreground"
        >
          <Settings className="h-4 w-4" />
          Settings
        </button>
      </div>

      <CreateSessionDialog open={createOpen} onOpenChange={setCreateOpen} />
      <AgentSettingsDialog open={settingsOpen} onOpenChange={setSettingsOpen} />
    </div>
  );
}
