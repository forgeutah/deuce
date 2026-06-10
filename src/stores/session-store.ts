import { create } from "zustand";
import { api } from "@/lib/api";
import { isSessionMember } from "@/lib/membership";
import type {
  Session,
  Project,
  Team,
  Message,
  ActivityItem,
  TabType,
  FileNode,
  User,
  TaskEventPayload,
  ActionEventPayload,
} from "@/types";
import {
  type SessionAgentRuns,
  type AgentRunEventType,
  emptyAgentRuns,
  applyEvent,
  applySnapshot,
} from "@/stores/agent-runs";

interface SessionState {
  // Data
  currentUser: User | null;
  teams: Team[];
  projects: Project[];
  sessions: Session[];
  messages: Record<string, Message[]>;
  activities: Record<string, ActivityItem[]>;
  fileTrees: Record<string, FileNode[]>;
  workspaceLogs: Record<string, string[]>;
  // Super Threads: per-session reduced agent-run (task/action) state.
  agentRuns: Record<string, SessionAgentRuns>;

  // UI state
  activeSessionId: string | null;
  activeTabMap: Record<string, TabType>;
  showLogs: boolean;
  searchQuery: string;
  // Super Threads: which session's deuce thread drawer is open (right panel).
  // Null when no drawer is open. Reset whenever the active session changes.
  openThread: { sessionId: string } | null;
  // steerSender is registered by the WebSocket hook (use-websocket) so the
  // store can forward steer/reply messages without ChatView needing its own
  // socket. Null until the hook mounts.
  steerSender: ((sessionId: string, message: string) => void) | null;
  // wsResubscribe is registered by the WebSocket hook so joining a session
  // (without switching the active session) can start the live subscription
  // immediately. Null until the hook mounts.
  wsResubscribe: ((sessionId: string) => void) | null;

  // Actions
  setActiveSession: (sessionId: string) => void;
  refreshFiles: (sessionId: string) => Promise<void>;
  setActiveTab: (sessionId: string, tab: TabType) => void;
  setSearchQuery: (query: string) => void;
  clearUnread: (sessionId: string) => void;
  addMessage: (message: Message) => void;
  addActivity: (activity: ActivityItem) => void;
  updateSessionPlan: (sessionId: string, content: string) => void;
  updateSessionDescription: (sessionId: string, description: string) => void;
  appendWorkspaceLog: (sessionId: string, line: string) => void;
  applyAgentRunEvent: (
    sessionId: string,
    type: AgentRunEventType,
    payload: TaskEventPayload | ActionEventPayload,
  ) => void;
  fetchAgentRuns: (sessionId: string) => Promise<void>;
  openAgentThread: (sessionId: string) => void;
  closeAgentThread: () => void;
  setSteerSender: (
    fn: ((sessionId: string, message: string) => void) | null,
  ) => void;
  steer: (sessionId: string, message: string) => void;
  setShowLogs: (show: boolean) => void;
  setWsResubscribe: (fn: ((sessionId: string) => void) | null) => void;
  joinSession: (sessionId: string) => Promise<void>;
  addSession: (session: Session) => void;
  updateWorkspaceStatus: (
    sessionId: string,
    status: Session["workspaceStatus"],
  ) => void;

  // Data setters
  setCurrentUser: (user: User | null) => void;
  setTeams: (teams: Team[]) => void;
  setProjects: (projects: Project[]) => void;
  setSessions: (sessions: Session[]) => void;
  setMessages: (sessionId: string, messages: Message[]) => void;
  setActivities: (sessionId: string, activities: ActivityItem[]) => void;
  setFileTrees: (sessionId: string, files: FileNode[]) => void;
}

// Per-session in-flight tracker for files refreshes. Lives outside the store
// so promises don't end up in reactive state; the dedupe just collapses
// overlapping fetches into a single network call.
const filesRefreshInFlight = new Map<string, Promise<void>>();

export const useSessionStore = create<SessionState>((set, get) => ({
  currentUser: null,
  teams: [],
  projects: [],
  sessions: [],
  messages: {},
  activities: {},
  fileTrees: {},
  workspaceLogs: {},
  agentRuns: {},

  activeSessionId: null,
  activeTabMap: {},
  showLogs: false,
  searchQuery: "",
  openThread: null,
  steerSender: null,
  wsResubscribe: null,

  setActiveSession: (sessionId) => {
    set((state) => ({
      activeSessionId: sessionId,
      // Switching channels closes any open agent-thread drawer — it belongs to
      // the session it was opened from.
      openThread:
        state.openThread?.sessionId === sessionId ? state.openThread : null,
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
    // Load files only when the workspace is ready — otherwise the backend
    // returns 409 and the call is pure waste.
    if (!state.fileTrees[sessionId]) {
      const session = state.sessions.find((s) => s.id === sessionId);
      if (session?.workspaceStatus === "ready") {
        get().refreshFiles(sessionId);
      }
    }
  },

  refreshFiles: (sessionId) => {
    const existing = filesRefreshInFlight.get(sessionId);
    if (existing) return existing;

    // Don't swallow the error — callers (refresh button, WS debounce) want
    // to surface failures distinctly. We still log because the WS path
    // doesn't await and would otherwise produce a silent unhandled rejection.
    const promise = api
      .listFiles(sessionId)
      .then((files) => {
        get().setFileTrees(sessionId, files);
      })
      .catch((err) => {
        console.error("[files] refresh failed", err);
        throw err;
      })
      .finally(() => {
        filesRefreshInFlight.delete(sessionId);
      });

    filesRefreshInFlight.set(sessionId, promise);
    return promise;
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

  applyAgentRunEvent: (sessionId, type, payload) => {
    const prev = get().agentRuns[sessionId] ?? emptyAgentRuns();
    const { state: next, needsResync } = applyEvent(prev, type, payload);
    set((state) => ({
      agentRuns: { ...state.agentRuns, [sessionId]: next },
    }));
    // A seq gap means an event was dropped (slow client / crash window) —
    // refetch the snapshot to reconcile (R10, mandatory recovery).
    if (needsResync) {
      void get().fetchAgentRuns(sessionId);
    }
  },

  fetchAgentRuns: async (sessionId) => {
    try {
      const snapshot = await api.getAgentRuns(sessionId);
      set((state) => ({
        agentRuns: { ...state.agentRuns, [sessionId]: applySnapshot(snapshot) },
      }));
    } catch (err) {
      console.error("failed to fetch agent runs snapshot", err);
    }
  },

  openAgentThread: (sessionId) => set({ openThread: { sessionId } }),

  closeAgentThread: () => set({ openThread: null }),

  setSteerSender: (fn) => set({ steerSender: fn }),

  steer: (sessionId, message) => {
    const send = get().steerSender;
    if (!send) {
      console.warn("steer dropped: no WebSocket sender registered");
      return;
    }
    send(sessionId, message);
  },

  setShowLogs: (show) => set({ showLogs: show }),

  setWsResubscribe: (fn) => set({ wsResubscribe: fn }),

  // joinSession adds the current user to a session's members (the "Join
  // Session" CTA). Optimistically flips membership so the composer unlocks
  // immediately, then reconciles with the server response. On success it
  // starts the live WS subscription (the active session didn't change, so the
  // join effect won't re-fire) and reloads messages to catch anything posted
  // between the static snapshot and going live. Rolls back on failure.
  joinSession: async (sessionId) => {
    const me = get().currentUser;
    if (me) {
      set((state) => ({
        sessions: state.sessions.map((s) =>
          s.id === sessionId && !isSessionMember(s, me.id)
            ? { ...s, members: [...s.members, me] }
            : s,
        ),
      }));
    }
    try {
      const updated = await api.joinSession(sessionId);
      set((state) => ({
        sessions: state.sessions.map((s) =>
          s.id === sessionId ? updated : s,
        ),
      }));
      // Reload the message snapshot BEFORE opening the live stream. setMessages
      // wholesale-replaces the array, so a live new_message arriving between
      // the snapshot fetch and the replace would be clobbered; subscribing
      // after the replace means live events only ever append via addMessage.
      const data = await api.listMessages(sessionId);
      get().setMessages(sessionId, data.messages.reverse());
      get().wsResubscribe?.(sessionId);
    } catch (err) {
      // Roll back the optimistic membership add.
      if (me) {
        set((state) => ({
          sessions: state.sessions.map((s) =>
            s.id === sessionId
              ? { ...s, members: s.members.filter((m) => m.id !== me.id) }
              : s,
          ),
        }));
      }
      throw err;
    }
  },

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

  setCurrentUser: (currentUser) => set({ currentUser }),
  setTeams: (teams) => set({ teams }),
  setProjects: (projects) => set({ projects }),
  setSessions: (sessions) =>
    set((state) => ({
      sessions,
      // If the active session is no longer visible (e.g. the user left its
      // team), clear the pointer so the UI doesn't strand on a dead view.
      activeSessionId:
        state.activeSessionId &&
        sessions.some((s) => s.id === state.activeSessionId)
          ? state.activeSessionId
          : null,
    })),
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
