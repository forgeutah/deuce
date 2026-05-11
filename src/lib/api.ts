const BASE = "/api";

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
    ...options,
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body?.error?.message ?? `Request failed: ${res.status}`);
  }

  return res.json();
}

export const api = {
  getMe: () => request<any>("/me"),

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
};
