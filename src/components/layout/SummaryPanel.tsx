import { useState } from "react";
import { Bot, Circle, UserPlus, Users as UsersIcon } from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { useSessionStore } from "@/stores/session-store";
import { ActivityFeed } from "@/components/activity/ActivityFeed";
import { ManageMembersDialog } from "@/components/session/ManageMembersDialog";
import type { Agent, User } from "@/types";

function AgentRow({ agent }: { agent: Agent }) {
  const statusStyles = {
    idle: "bg-neutral-8",
    working: "bg-success animate-pulse-dot",
    "warming-up": "bg-warning",
    error: "bg-danger",
  }[agent.status];

  return (
    <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
      <div
        className="flex h-6 w-6 items-center justify-center rounded text-xs font-semibold text-foreground-on-emphasis"
        style={{ backgroundColor: agent.color }}
      >
        {agent.name[0]}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="text-xs font-medium text-foreground truncate">
            {agent.name}
          </span>
          <span className={cn("h-2 w-2 rounded-full shrink-0", statusStyles)} />
        </div>
        <span className="text-[10px] text-foreground-subtle">{agent.description}</span>
      </div>
    </div>
  );
}

function UserRow({ user }: { user: User }) {
  return (
    <div className="flex items-center gap-2 rounded-md px-2 py-1.5">
      <div className="relative">
        <img
          src={user.avatar}
          alt={user.name}
          className="h-6 w-6 rounded-full"
        />
        <Circle
          className={cn(
            "absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 fill-current stroke-background-subtle stroke-2",
            user.status === "online" ? "text-success" : "text-neutral-7",
          )}
        />
      </div>
      <span className="text-xs font-medium text-foreground truncate">
        {user.name}
      </span>
    </div>
  );
}

export function SummaryPanel() {
  const { activeSessionId, sessions, activities } = useSessionStore();
  const [membersOpen, setMembersOpen] = useState(false);

  if (!activeSessionId) {
    return (
      <div className="flex h-full items-center justify-center p-4">
        <p className="text-xs text-foreground-subtle text-center">
          Select a session to see participants and activity
        </p>
      </div>
    );
  }

  const session = sessions.find((s) => s.id === activeSessionId);
  if (!session) return null;

  const sessionActivities = activities[activeSessionId] ?? [];

  return (
    <div className="flex h-full flex-col">
      {/* Participants */}
      <div className="p-3">
        <div className="flex items-center gap-1.5 mb-2">
          <UsersIcon className="h-3.5 w-3.5 text-foreground-subtle" />
          <h3 className="text-xs font-semibold uppercase tracking-wider text-foreground-subtle">
            Participants
          </h3>
          <span className="text-[10px] text-foreground-subtle">
            ({session.agents.length + session.members.length})
          </span>
        </div>

        {/* Agents */}
        {session.agents.length > 0 && (
          <div className="mb-2">
            <div className="flex items-center gap-1 mb-1 px-2">
              <Bot className="h-3 w-3 text-foreground-subtle" />
              <span className="text-[10px] text-foreground-subtle">Agents</span>
            </div>
            <div className="flex flex-col gap-0.5">
              {session.agents.map((agent) => (
                <AgentRow key={agent.id} agent={agent} />
              ))}
            </div>
          </div>
        )}

        {/* Humans */}
        <div>
          <div className="flex items-center gap-1 mb-1 px-2">
            <UsersIcon className="h-3 w-3 text-foreground-subtle" />
            <span className="text-[10px] text-foreground-subtle">Members</span>
            <button
              onClick={() => setMembersOpen(true)}
              title="Add or remove members"
              className="ml-auto flex items-center gap-1 rounded px-1 py-0.5 text-[10px] text-foreground-subtle hover:bg-background-hover hover:text-foreground"
            >
              <UserPlus className="h-3 w-3" />
              Add
            </button>
          </div>
          <div className="flex flex-col gap-0.5">
            {session.members.map((user) => (
              <UserRow key={user.id} user={user} />
            ))}
          </div>
        </div>
      </div>

      <ManageMembersDialog
        session={session}
        open={membersOpen}
        onOpenChange={setMembersOpen}
      />

      <Separator className="bg-border-muted" />

      {/* Activity Feed */}
      <div className="flex-1 overflow-hidden p-3">
        <h3 className="mb-2 text-xs font-semibold uppercase tracking-wider text-foreground-subtle">
          Activity
        </h3>
        <ScrollArea className="h-full">
          <ActivityFeed activities={sessionActivities} />
        </ScrollArea>
      </div>
    </div>
  );
}
