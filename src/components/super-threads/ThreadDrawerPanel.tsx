// ThreadDrawerPanel — store-connected wrapper that renders the session's deuce
// thread drawer when it is open. AppShell mounts this in the right panel in
// place of the SummaryPanel when openThread is set.

import { useSessionStore } from "@/stores/session-store";
import { sessionTaskList, queuePositions } from "@/stores/agent-runs";
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
  if (!session) return null;

  const runs = agentRuns[openThread.sessionId];
  const tasks = sessionTaskList(runs);
  const qpos = queuePositions(runs);

  // requestedBy ids resolve against session members plus the current user.
  const userIndex = new Map<string, Pick<User, "name" | "avatar">>();
  for (const m of session.members) userIndex.set(m.id, m);
  if (currentUser) userIndex.set(currentUser.id, currentUser);
  const lookupUser = (id?: string) => (id ? userIndex.get(id) : undefined);

  return (
    <AgentThreadDrawer
      sessionId={openThread.sessionId}
      tasks={tasks}
      queuePositions={qpos}
      lookupUser={lookupUser}
      onClose={closeAgentThread}
      onSend={(message) => steer(openThread.sessionId, message)}
    />
  );
}
