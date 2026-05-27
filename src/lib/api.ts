import type {
  ActivityItem,
  Agent,
  FileContentResponse,
  FileNode,
  Message,
  Project,
  Session,
  SSHKey,
  Team,
  User,
} from "@/types";

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

interface MessagesPage {
  messages: Message[];
  cursor: string;
  hasMore: boolean;
}

interface AgentMutation {
  name: string;
  role: string;
  provider: string;
  model: string;
  description: string;
  systemPrompt: string;
}

interface CreateSessionBody {
  name: string;
  description?: string;
  projectId: string;
  repoUrl?: string;
  agentIds: string[];
  memberIds: string[];
}

interface UpdateSessionBody {
  status?: string;
  planContent?: string;
  workspaceStatus?: string;
  description?: string;
}

interface SendMessageBody {
  content: string;
  mentions: string[];
}

const BASE = "/api";

/**
 * ApiError carries the HTTP status and the server-supplied error code
 * alongside the message. Callers can branch on `code` (e.g. NOT_AUTHORIZED)
 * or `status` (e.g. 403) to render specific UI rather than reading the
 * message string.
 */
export class ApiError extends Error {
  status: number;
  code: string;

  constructor(message: string, status: number, code: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

async function request<T>(
  path: string,
  options?: RequestInit & { signal?: AbortSignal },
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
    ...options,
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const message =
      body?.error?.message ?? `Request failed: ${res.status}`;
    const code = body?.error?.code ?? "";
    throw new ApiError(message, res.status, code);
  }

  return res.json();
}

export const api = {
  getMe: () => request<User>("/me"),

  updateMyName: (name: string) =>
    request<User>("/me", {
      method: "PATCH",
      body: JSON.stringify({ name }),
    }),

  listMySSHKeys: () => request<SSHKey[]>("/me/ssh-keys"),

  createMySSHKey: (label: string, publicKey: string) =>
    request<SSHKey>("/me/ssh-keys", {
      method: "POST",
      body: JSON.stringify({ label, publicKey }),
    }),

  deleteMySSHKey: (keyID: string) =>
    request<void>(`/me/ssh-keys/${keyID}`, { method: "DELETE" }),

  listTeams: () => request<Team[]>("/teams"),

  listProjects: (teamId?: string) =>
    request<Project[]>(teamId ? `/projects?teamId=${teamId}` : "/projects"),

  listAgents: () => request<Agent[]>("/agents"),

  createAgent: (body: AgentMutation) =>
    request<Agent>("/agents", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateAgent: (id: string, body: AgentMutation) =>
    request<Agent>(`/agents/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  deleteAgent: (id: string) =>
    request<void>(`/agents/${id}`, { method: "DELETE" }),

  stopAgent: (sessionId: string) =>
    request<void>(`/sessions/${sessionId}/agents/stop`, { method: "POST" }),

  listSessions: () => request<Session[]>("/sessions"),

  getSession: (id: string) => request<Session>(`/sessions/${id}`),

  createSession: (body: CreateSessionBody) =>
    request<Session>("/sessions", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateSession: (id: string, body: UpdateSessionBody) =>
    request<Session>(`/sessions/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  listMessages: (sessionId: string, before?: string, limit = 50) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (before) params.set("before", before);
    return request<MessagesPage>(
      `/sessions/${sessionId}/messages?${params}`,
    );
  },

  sendMessage: (sessionId: string, body: SendMessageBody) =>
    request<Message>(`/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  listActivities: (sessionId: string, limit = 20) =>
    request<ActivityItem[]>(`/sessions/${sessionId}/activities?limit=${limit}`),

  updateSessionAgents: (sessionId: string, agentIds: string[]) =>
    request<Agent[]>(`/sessions/${sessionId}/agents`, {
      method: "PUT",
      body: JSON.stringify({ agentIds }),
    }),

  listGitHubOrgs: () => request<GitHubOrg[]>("/github/orgs"),

  listGitHubRepos: (owner: string) =>
    request<GitHubRepo[]>(`/github/repos?owner=${encodeURIComponent(owner)}`),

  listFiles: (sessionId: string) =>
    request<FileNode[]>(`/sessions/${sessionId}/files`),

  getFileContent: (
    sessionId: string,
    path: string,
    signal?: AbortSignal,
  ) =>
    request<FileContentResponse>(
      `/sessions/${sessionId}/files/content?path=${encodeURIComponent(path)}`,
      { signal },
    ),
};
