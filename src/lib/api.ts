import type { FileNode, FileContentResponse, User } from "@/types";

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

  listTeams: () => request<any[]>("/teams"),

  listProjects: (teamId?: string) =>
    request<any[]>(teamId ? `/projects?teamId=${teamId}` : "/projects"),

  listAgents: () => request<any[]>("/agents"),

  createAgent: (body: {
    name: string;
    role: string;
    provider: string;
    model: string;
    description: string;
    systemPrompt: string;
  }) =>
    request<any>("/agents", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateAgent: (
    id: string,
    body: {
      name: string;
      role: string;
      provider: string;
      model: string;
      description: string;
      systemPrompt: string;
    },
  ) =>
    request<any>(`/agents/${id}`, {
      method: "PUT",
      body: JSON.stringify(body),
    }),

  deleteAgent: (id: string) =>
    request<void>(`/agents/${id}`, { method: "DELETE" }),

  stopAgent: (sessionId: string) =>
    request<void>(`/sessions/${sessionId}/agents/stop`, { method: "POST" }),

  listSessions: () => request<any[]>("/sessions"),

  getSession: (id: string) => request<any>(`/sessions/${id}`),

  createSession: (body: {
    name: string;
    description?: string;
    projectId: string;
    repoUrl?: string;
    agentIds: string[];
    memberIds: string[];
  }) =>
    request<any>("/sessions", {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateSession: (
    id: string,
    body: {
      status?: string;
      planContent?: string;
      workspaceStatus?: string;
      description?: string;
    },
  ) =>
    request<any>(`/sessions/${id}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  listMessages: (sessionId: string, before?: string, limit = 50) => {
    const params = new URLSearchParams({ limit: String(limit) });
    if (before) params.set("before", before);
    return request<{ messages: any[]; cursor: string; hasMore: boolean }>(
      `/sessions/${sessionId}/messages?${params}`,
    );
  },

  sendMessage: (
    sessionId: string,
    body: { content: string; mentions: string[] },
  ) =>
    request<any>(`/sessions/${sessionId}/messages`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  listActivities: (sessionId: string, limit = 20) =>
    request<any[]>(`/sessions/${sessionId}/activities?limit=${limit}`),

  updateSessionAgents: (sessionId: string, agentIds: string[]) =>
    request<any[]>(`/sessions/${sessionId}/agents`, {
      method: "PUT",
      body: JSON.stringify({ agentIds }),
    }),

  listGitHubOrgs: () =>
    request<{ login: string; avatarUrl: string }[]>("/github/orgs"),

  listGitHubRepos: (owner: string) =>
    request<
      {
        name: string;
        fullName: string;
        cloneUrl: string;
        description: string;
        language: string;
        private: boolean;
        defaultBranch: string;
      }[]
    >(`/github/repos?owner=${encodeURIComponent(owner)}`),

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
