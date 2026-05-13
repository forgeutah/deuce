export type TabType = "chat" | "plan" | "files" | "terminal";

export type SessionStatus = "active" | "paused" | "archived";
export type WorkspaceStatus = "starting" | "ready" | "failed" | "suspended";
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
