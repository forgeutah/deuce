import { create } from "zustand";
import { api } from "@/lib/api";
import type {
  Session,
  Project,
  Team,
  Message,
  Agent,
  ActivityItem,
  TabType,
  FileNode,
} from "@/types";

interface SessionState {
  // Data
  teams: Team[];
  projects: Project[];
  sessions: Session[];
  messages: Record<string, Message[]>;
  activities: Record<string, ActivityItem[]>;
  fileTrees: Record<string, FileNode[]>;
  thinkingAgents: Record<string, string[]>;
  workspaceLogs: Record<string, string[]>;
  agentOutput: Record<string, { agentId: string; content: string; contentType: string }[]>;

  // UI state
  activeSessionId: string | null;
  activeTabMap: Record<string, TabType>;
  showLogs: boolean;
  searchQuery: string;

  // Actions
  setActiveSession: (sessionId: string) => void;
  setActiveTab: (sessionId: string, tab: TabType) => void;
  setSearchQuery: (query: string) => void;
  clearUnread: (sessionId: string) => void;
  addMessage: (message: Message) => void;
  setThinkingAgent: (sessionId: string, agentId: string) => void;
  clearThinkingAgent: (sessionId: string, agentId: string) => void;
  updateAgentStatus: (
    sessionId: string,
    agentId: string,
    status: Agent["status"],
  ) => void;
  addActivity: (activity: ActivityItem) => void;
  updateSessionPlan: (sessionId: string, content: string) => void;
  updateSessionDescription: (sessionId: string, description: string) => void;
  appendWorkspaceLog: (sessionId: string, line: string) => void;
  appendAgentOutput: (sessionId: string, output: { agentId: string; content: string; contentType: string }) => void;
  clearAgentOutput: (sessionId: string) => void;
  setShowLogs: (show: boolean) => void;
  addSession: (session: Session) => void;
  updateWorkspaceStatus: (
    sessionId: string,
    status: Session["workspaceStatus"],
  ) => void;

  // Data setters
  setTeams: (teams: Team[]) => void;
  setProjects: (projects: Project[]) => void;
  setSessions: (sessions: Session[]) => void;
  setMessages: (sessionId: string, messages: Message[]) => void;
  setActivities: (sessionId: string, activities: ActivityItem[]) => void;
  setFileTrees: (sessionId: string, files: FileNode[]) => void;
}

export const useSessionStore = create<SessionState>((set, get) => ({
  teams: [],
  projects: [],
  sessions: [],
  messages: {},
  activities: {},
  fileTrees: {},
  thinkingAgents: {},
  workspaceLogs: {},
  agentOutput: {},

  activeSessionId: null,
  activeTabMap: {},
  showLogs: false,
  searchQuery: "",

  setActiveSession: (sessionId) => {
    set((state) => ({
      activeSessionId: sessionId,
      sessions: state.sessions.map((s) =>
        s.id === sessionId ? { ...s, unreadCount: 0 } : s,
      ),
    }));

    // Load messages and activities from API if not already loaded
    const state = get();
    if (!state.messages[sessionId]) {
      api.listMessages(sessionId).then((data) => {
        get().setMessages(sessionId, data.messages.reverse());
      }).catch(console.error);
    }
    if (!state.activities[sessionId]) {
      api.listActivities(sessionId).then((activities) => {
        get().setActivities(sessionId, activities);
      }).catch(console.error);
    }
  },

  setActiveTab: (sessionId, tab) =>
    set((state) => ({
      activeTabMap: { ...state.activeTabMap, [sessionId]: tab },
    })),

  setSearchQuery: (query) => set({ searchQuery: query }),

  clearUnread: (sessionId) =>
    set((state) => ({
      sessions: state.sessions.map((s) =>
        s.id === sessionId ? { ...s, unreadCount: 0 } : s,
      ),
    })),

  addMessage: (message) =>
    set((state) => {
      const sessionMessages = state.messages[message.sessionId] ?? [];
      // Deduplicate by ID
      if (sessionMessages.some((m) => m.id === message.id)) {
        return state;
      }
      return {
        messages: {
          ...state.messages,
          [message.sessionId]: [...sessionMessages, message],
        },
        sessions: state.sessions.map((s) =>
          s.id === message.sessionId
            ? {
                ...s,
                lastActivityAt: message.createdAt,
                unreadCount:
                  s.id === state.activeSessionId ? 0 : s.unreadCount + 1,
              }
            : s,
        ),
      };
    }),

  setThinkingAgent: (sessionId, agentId) =>
    set((state) => {
      const current = state.thinkingAgents[sessionId] ?? [];
      if (current.includes(agentId)) return state;
      return {
        thinkingAgents: {
          ...state.thinkingAgents,
          [sessionId]: [...current, agentId],
        },
      };
    }),

  clearThinkingAgent: (sessionId, agentId) =>
    set((state) => {
      const current = state.thinkingAgents[sessionId] ?? [];
      return {
        thinkingAgents: {
          ...state.thinkingAgents,
          [sessionId]: current.filter((id) => id !== agentId),
        },
      };
    }),

  updateAgentStatus: (sessionId, agentId, status) =>
    set((state) => ({
      sessions: state.sessions.map((s) =>
        s.id === sessionId
          ? {
              ...s,
              agents: s.agents.map((a) =>
                a.id === agentId ? { ...a, status } : a,
              ),
            }
          : s,
      ),
    })),

  addActivity: (activity) =>
    set((state) => {
      const current = state.activities[activity.sessionId] ?? [];
      if (current.some((a) => a.id === activity.id)) return state;
      return {
        activities: {
          ...state.activities,
          [activity.sessionId]: [activity, ...current],
        },
      };
    }),

  updateSessionPlan: (sessionId, content) =>
    set((state) => ({
      sessions: state.sessions.map((s) =>
        s.id === sessionId ? { ...s, planContent: content } : s,
      ),
    })),

  updateSessionDescription: (sessionId, description) =>
    set((state) => ({
      sessions: state.sessions.map((s) =>
        s.id === sessionId ? { ...s, description } : s,
      ),
    })),

  appendWorkspaceLog: (sessionId, line) =>
    set((state) => {
      const current = state.workspaceLogs[sessionId] ?? [];
      return {
        workspaceLogs: {
          ...state.workspaceLogs,
          [sessionId]: [...current, line],
        },
      };
    }),

  appendAgentOutput: (sessionId, output) =>
    set((state) => {
      const current = state.agentOutput[sessionId] ?? [];
      return {
        agentOutput: {
          ...state.agentOutput,
          [sessionId]: [...current, output],
        },
      };
    }),

  clearAgentOutput: (sessionId) =>
    set((state) => ({
      agentOutput: { ...state.agentOutput, [sessionId]: [] },
    })),

  setShowLogs: (show) => set({ showLogs: show }),

  addSession: (session) =>
    set((state) => ({
      sessions: [session, ...state.sessions],
    })),

  updateWorkspaceStatus: (sessionId, status) =>
    set((state) => ({
      sessions: state.sessions.map((s) =>
        s.id === sessionId ? { ...s, workspaceStatus: status } : s,
      ),
    })),

  setTeams: (teams) => set({ teams }),
  setProjects: (projects) => set({ projects }),
  setSessions: (sessions) => set({ sessions }),
  setMessages: (sessionId, messages) =>
    set((state) => ({
      messages: { ...state.messages, [sessionId]: messages },
    })),
  setActivities: (sessionId, activities) =>
    set((state) => ({
      activities: { ...state.activities, [sessionId]: activities },
    })),
  setFileTrees: (sessionId, files) =>
    set((state) => ({
      fileTrees: { ...state.fileTrees, [sessionId]: files },
    })),
}));
