// ThreadDrawerPanel — store-connected wrapper that renders the AgentThreadDrawer
// for whichever agent thread is currently open. AppShell mounts this in the
// right panel in place of the SummaryPanel when openThread is set.

import { useSessionStore } from "@/stores/session-store";
import { tasksForAgent, queuePositions } from "@/stores/agent-runs";
import type { User } from "@/types";
import { AgentThreadDrawer } from "./AgentThreadDrawer";

export function ThreadDrawerPanel() {
  const openThread = useSessionStore((s) => s.openThread);
  const sessions = useSessionStore((s) => s.sessions);
  const agentRuns = useSessionStore((s) => s.agentRuns);
  const currentUser = useSessionStore((s) => s.currentUser);
  const closeAgentThread = useSessionStore((s) => s.closeAgentThread);
  const steer = useSessionStore((s) => s.steer);

  if (!openThread) return null;

  const session = sessions.find((s) => s.id === openThread.sessionId);
  const agent = session?.agents.find((a) => a.id === openThread.agentId);
  if (!session || !agent) return null;

  const runs = agentRuns[openThread.sessionId];
  const tasks = tasksForAgent(runs, agent.id);
  const qpos = queuePositions(runs);

  // requestedBy ids resolve against session members plus the current user.
  const userIndex = new Map<string, Pick<User, "name" | "avatar">>();
  for (const m of session.members) userIndex.set(m.id, m);
  if (currentUser) userIndex.set(currentUser.id, currentUser);
  const lookupUser = (id?: string) => (id ? userIndex.get(id) : undefined);

  return (
    <AgentThreadDrawer
      agent={agent}
      tasks={tasks}
      queuePositions={qpos}
      lookupUser={lookupUser}
      onClose={closeAgentThread}
      onSend={(message) => steer(openThread.sessionId, agent.id, message)}
    />
  );
}
