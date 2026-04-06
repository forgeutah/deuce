import { create } from "zustand";
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
  messages: Record<string, Message[]>; // sessionId -> messages
  activities: Record<string, ActivityItem[]>; // sessionId -> activities
  fileTrees: Record<string, FileNode[]>; // sessionId -> file tree
  thinkingAgents: Record<string, string[]>; // sessionId -> agent IDs currently thinking

  // UI state
  activeSessionId: string | null;
  activeTabMap: Record<string, TabType>; // sessionId -> active tab
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
  addSession: (session: Session) => void;
  updateWorkspaceStatus: (
    sessionId: string,
    status: Session["workspaceStatus"],
  ) => void;

  // Data setters (for mock initialization)
  setTeams: (teams: Team[]) => void;
  setProjects: (projects: Project[]) => void;
  setSessions: (sessions: Session[]) => void;
  setMessages: (sessionId: string, messages: Message[]) => void;
  setActivities: (sessionId: string, activities: ActivityItem[]) => void;
  setFileTrees: (sessionId: string, files: FileNode[]) => void;
}

export const useSessionStore = create<SessionState>((set) => ({
  teams: [],
  projects: [],
  sessions: [],
  messages: {},
  activities: {},
  fileTrees: {},
  thinkingAgents: {},

  activeSessionId: null,
  activeTabMap: {},
  searchQuery: "",

  setActiveSession: (sessionId) =>
    set((state) => ({
      activeSessionId: sessionId,
      sessions: state.sessions.map((s) =>
        s.id === sessionId ? { ...s, unreadCount: 0 } : s,
      ),
    })),

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
                  s.id === state.activeSessionId
                    ? 0
                    : s.unreadCount + 1,
              }
            : s,
        ),
      };
    }),

  setThinkingAgent: (sessionId, agentId) =>
    set((state) => {
      const current = state.thinkingAgents[sessionId] ?? [];
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
