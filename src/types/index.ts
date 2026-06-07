export type TabType = "chat" | "plan" | "files" | "terminal";

export type SessionStatus = "active" | "paused" | "archived";
export type WorkspaceStatus =
  | "starting"
  | "ready"
  | "stopping"
  | "stopped"
  | "rebuilding"
  | "deleting"
  | "missing"
  | "failed";
export type AgentStatus = "idle" | "working" | "warming-up" | "error";
export type UserStatus = "online" | "offline";
export type MessageStatus = "sent" | "thinking" | "error";
export type AuthorType = "human" | "agent";

export type AgentRole = string;

export interface Team {
  id: string;
  name: string;
  slug: string;
  members: User[];
}

export interface Project {
  id: string;
  name: string;
  repoUrl: string;
  teamId: string;
}

export interface Session {
  id: string;
  name: string;
  description: string;
  projectId: string;
  status: SessionStatus;
  agents: Agent[];
  members: User[];
  unreadCount: number;
  createdAt: string;
  lastActivityAt: string;
  workspaceStatus: WorkspaceStatus;
  planContent: string;
}

export interface Message {
  id: string;
  sessionId: string;
  authorId: string;
  authorType: AuthorType;
  content: string;
  expandableContent?: ExpandableContent[];
  mentions: string[];
  createdAt: string;
  status: MessageStatus;
}

export interface ExpandableContent {
  type: "diff" | "test-results" | "terminal-output";
  title: string;
  summary: string;
  content: string;
}

export interface Agent {
  id: string;
  name: string;
  role: AgentRole;
  color: string;
  colorMuted: string;
  status: AgentStatus;
  provider: string;
  model: string;
  description: string;
  systemPrompt: string;
}

export interface User {
  id: string;
  name: string;
  email: string;
  avatar: string;
  status: UserStatus;
}

export type GitStatus = "M" | "U" | "A" | "D";

export interface FileNode {
  id: string;
  name: string;
  path: string;
  type: "file" | "directory";
  children?: FileNode[];
  content?: string;
  language?: string;
  modifiedBy?: string;
  gitStatus?: GitStatus;
  isRepoRoot?: boolean;
}

export interface FileContentResponse {
  path: string;
  content: string;
  isBinary: boolean;
  truncated: boolean;
  size: number;
}

export interface ActivityItem {
  id: string;
  sessionId: string;
  type: "file-change" | "test-run" | "commit" | "agent-action";
  description: string;
  timestamp: string;
  agentId?: string;
  metadata?: Record<string, string>;
}

// SSHKey is the wire shape for a user-registered SSH public key.
// `publicKey` is only present on the create response (the R15 inline
// confirmation); list/get responses omit it so a compromised client
// cannot snapshot every key on file. `lastUsedAt` is null until the
// proxy auth callback touches it.
export interface SSHKey {
  id: string;
  label: string;
  fingerprint: string;
  publicKey?: string;
  createdAt: string;
  lastUsedAt: string | null;
}

// --- Super Threads: agent tasks, actions, and the AgentRunEvent stream ---

export type TaskState =
  | "queued"
  | "running"
  | "awaiting_input"
  | "done"
  | "failed"
  | "cancelled";

export type ActionStatus = "started" | "completed" | "error" | "interrupted";

// AgentAction is one tool call within a task's action log, correlated by callId.
export interface AgentAction {
  callId: string;
  seq: number;
  tool: string; // Read | Grep | Edit | Write | Bash | Think | ...
  arg?: string;
  text?: string;
  stat?: string;
  status: ActionStatus;
  isError?: boolean;
}

// AgentTask is one @mention-spawned agent run, anchored to a channel message.
export interface AgentTask {
  id: string;
  sessionId: string;
  agentId: string;
  requestedBy?: string;
  anchorMessageId?: string;
  prompt: string;
  state: TaskState;
  seq: number;
  position?: number; // queue #N while queued
  pendingQuestion?: string;
  reply?: string;
  actions: AgentAction[];
  // order is a client-only stable creation-order index assigned by the reducer
  // (not sent by the server). Sorting on it keeps the drawer thread and queue
  // positions chronological even as later events overwrite seq.
  order?: number;
}

// AgentRunEvent payloads mirror Go ws.TaskEventPayload / ws.ActionEventPayload.
// The reducer applies them by seq; a gap triggers a snapshot refetch.
export interface TaskEventPayload {
  seq: number;
  taskId: string;
  agentId: string;
  requestedBy?: string;
  anchorMessageId?: string;
  prompt?: string;
  state?: TaskState;
  position?: number;
  pendingQuestion?: string;
  reply?: string;
  status?: "done" | "failed" | "cancelled";
}

export interface ActionEventPayload {
  seq: number;
  taskId: string;
  agentId: string;
  callId: string;
  tool?: string;
  arg?: string;
  text?: string;
  stat?: string;
  isError?: boolean;
}

// Snapshot returned by GET /sessions/:id/agent-runs (R9): current task+action
// state plus the latest seq the client should resume strictly after.
export interface AgentRunSnapshot {
  tasks: AgentTask[];
  latestSeq: number;
}
