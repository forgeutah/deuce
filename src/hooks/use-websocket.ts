import { useEffect, useRef, useCallback } from "react";
import { useSessionStore } from "@/stores/session-store";
import type {
  AgentStatus,
  Message,
  ActivityItem,
  TaskEventPayload,
  ActionEventPayload,
} from "@/types";
import { isAgentRunEvent } from "@/stores/agent-runs";
import { api } from "@/lib/api";

interface ServerMessage {
  type: string;
  sessionId: string;
  // Per-type discriminated narrowing happens inside the switch — payloads
  // vary by `type` and the server is the source of truth. `unknown` forces
  // callers to assert the expected shape instead of silently accepting any.
  payload: unknown;
}

// Per-session trailing-edge debounce for files refreshes triggered by
// activity_update. Module-scoped so it survives hook re-renders.
const filesRefreshTimers = new Map<string, ReturnType<typeof setTimeout>>();
const FILES_REFRESH_DEBOUNCE_MS = 500;

function scheduleFilesRefresh(sessionId: string) {
  const existing = filesRefreshTimers.get(sessionId);
  if (existing) clearTimeout(existing);
  const timer = setTimeout(() => {
    filesRefreshTimers.delete(sessionId);
    // refreshFiles now re-throws on failure so the refresh button can show
    // an error — but the WS-driven call doesn't await it. Catch here to
    // suppress the unhandled-rejection.
    useSessionStore
      .getState()
      .refreshFiles(sessionId)
      .catch(() => {});
  }, FILES_REFRESH_DEBOUNCE_MS);
  filesRefreshTimers.set(sessionId, timer);
}

// Cap consecutive reconnect attempts. Without this, a user whose role is
// revoked mid-session (or whose secret rotates without their tab being
// reloaded) generates a perpetual reconnect storm against a server that will
// keep rejecting the upgrade with 403. Browsers do not expose the HTTP
// status of a failed WebSocket upgrade to JS, so we cannot distinguish a
// transient network blip from an auth-permanent rejection — capping the
// attempt count covers both cases gracefully. The user can recover by
// reloading the tab, which re-runs the boot flow and surfaces the
// NotAuthorizedView if applicable.
const MAX_RECONNECT_ATTEMPTS = 20;

export function useWebSocket() {
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const reconnectDelay = useRef(1000);
  const reconnectAttempts = useRef(0);
  const activeSessionRef = useRef<string | null>(null);

  const {
    activeSessionId,
    addMessage,
    setThinkingAgent,
    clearThinkingAgent,
    updateAgentStatus,
    addActivity,
    appendWorkspaceLog,
    appendAgentOutput,
    clearAgentOutput,
  } = useSessionStore();

  // Refs hold the latest version of handleMessage and connect so the
  // long-lived ws.onmessage / ws.onclose closures always reach the current
  // function bodies instead of stale ones captured at first connect. This
  // also breaks the connect/handleMessage capture cycle that the
  // react-hooks/immutability rule otherwise flags.
  const handleMessageRef = useRef<(msg: ServerMessage) => void>(() => {});
  const connectRef = useRef<() => void>(() => {});

  const handleMessage = useCallback(
    (msg: ServerMessage) => {
      switch (msg.type) {
        case "new_message": {
          const message = msg.payload as Message;
          // Normalize the response from server (camelCase mapping)
          const normalized: Message = {
            id: message.id,
            sessionId: msg.sessionId || message.sessionId,
            authorId: message.authorId,
            authorType: message.authorType,
            content: message.content,
            expandableContent: message.expandableContent,
            mentions: message.mentions ?? [],
            createdAt: message.createdAt,
            status: message.status ?? "sent",
          };
          addMessage(normalized);
          break;
        }

        case "agent_status": {
          const { agentId, status } = msg.payload as {
            agentId: string;
            status: AgentStatus;
          };
          updateAgentStatus(msg.sessionId, agentId, status);
          // Clear streaming output when agent finishes
          if (status === "idle" || status === "error") {
            clearAgentOutput(msg.sessionId);
          }
          break;
        }

        case "typing_indicator": {
          const { agentId, active } = msg.payload as {
            agentId: string;
            active: boolean;
          };
          if (active) {
            setThinkingAgent(msg.sessionId, agentId);
          } else {
            clearThinkingAgent(msg.sessionId, agentId);
          }
          break;
        }

        case "activity_update": {
          const activity = msg.payload as ActivityItem;
          const sessionId = msg.sessionId || activity.sessionId;
          addActivity({
            ...activity,
            sessionId,
          });
          // Debounced refresh — every activity may have touched files. The
          // backend's per-session walk lock collapses any overlap if the
          // debounce window misses.
          scheduleFilesRefresh(sessionId);
          break;
        }

        case "agent_output": {
          const { agentId, content, contentType } = msg.payload as {
            agentId: string;
            content: string;
            contentType: string;
          };
          appendAgentOutput(msg.sessionId, { agentId, content, contentType });
          break;
        }

        case "workspace_log": {
          const { line } = msg.payload as { line: string };
          appendWorkspaceLog(msg.sessionId, line);
          break;
        }

        case "session_update": {
          // Refresh the session list to pick up changes
          api.listSessions().then((sessions) => {
            useSessionStore.getState().setSessions(sessions);
          });
          break;
        }

        case "unread_update": {
          const { unreadCount } = msg.payload as { unreadCount: number };
          const store = useSessionStore.getState();
          store.setSessions(
            store.sessions.map((s) =>
              s.id === msg.sessionId ? { ...s, unreadCount } : s,
            ),
          );
          break;
        }

        default: {
          // Super Threads AgentRunEvent family (task_*/action_*): apply by seq
          // to the per-session reduced state; the reducer self-heals seq gaps
          // by refetching the snapshot.
          if (isAgentRunEvent(msg.type)) {
            useSessionStore
              .getState()
              .applyAgentRunEvent(
                msg.sessionId,
                msg.type,
                msg.payload as TaskEventPayload | ActionEventPayload,
              );
          }
          break;
        }
      }
    },
    [addMessage, updateAgentStatus, setThinkingAgent, clearThinkingAgent, addActivity, appendWorkspaceLog, appendAgentOutput, clearAgentOutput],
  );

  // Keep the latest handleMessage reachable from the long-lived ws.onmessage
  // closure without forcing connect to re-create on every render.
  useEffect(() => {
    handleMessageRef.current = handleMessage;
  }, [handleMessage]);

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

    ws.onopen = () => {
      console.log("[ws] connected");
      reconnectDelay.current = 1000;
      reconnectAttempts.current = 0;

      // Re-join active session, then fetch its agent-run snapshot. This covers
      // the initial page load / refresh: the active session is typically set
      // BEFORE the socket finishes opening, so the join effect below early-
      // returns without fetching. Without this fetch, inline task cards stay
      // empty until the user switches channels. Subscribe before the fetch so
      // any event during the round-trip is applied or gap-healed (R9).
      if (activeSessionRef.current) {
        ws.send(
          JSON.stringify({
            type: "join",
            sessionId: activeSessionRef.current,
          }),
        );
        void useSessionStore.getState().fetchAgentRuns(activeSessionRef.current);
      }
    };

    ws.onmessage = (event) => {
      try {
        const msg: ServerMessage = JSON.parse(event.data);
        handleMessageRef.current(msg);
      } catch {
        console.warn("[ws] failed to parse message", event.data);
      }
    };

    ws.onclose = () => {
      wsRef.current = null;
      if (reconnectAttempts.current >= MAX_RECONNECT_ATTEMPTS) {
        console.warn(
          `[ws] disconnected, max reconnect attempts (${MAX_RECONNECT_ATTEMPTS}) reached — giving up. Reload the page to retry.`,
        );
        return;
      }
      console.log("[ws] disconnected, reconnecting...");
      reconnectTimer.current = setTimeout(() => {
        reconnectAttempts.current += 1;
        reconnectDelay.current = Math.min(reconnectDelay.current * 2, 30000);
        connectRef.current();
      }, reconnectDelay.current);
    };

    ws.onerror = (err) => {
      console.error("[ws] error", err);
      ws.close();
    };

    wsRef.current = ws;
  }, []);

  // Keep connectRef pointed at the latest connect so the reconnect timer
  // captured in ws.onclose calls the current function.
  useEffect(() => {
    connectRef.current = connect;
  }, [connect]);

  // Connect on mount
  useEffect(() => {
    connect();
    return () => {
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current);
      wsRef.current?.close();
    };
  }, [connect]);

  // Join/leave sessions when active session changes
  useEffect(() => {
    const ws = wsRef.current;
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      activeSessionRef.current = activeSessionId;
      return;
    }

    // Leave previous session
    if (activeSessionRef.current && activeSessionRef.current !== activeSessionId) {
      ws.send(
        JSON.stringify({
          type: "leave",
          sessionId: activeSessionRef.current,
        }),
      );
    }

    // Join new session, then fetch the agent-run snapshot. Subscribe BEFORE
    // the snapshot fetch so any event broadcast during the fetch is applied
    // (or self-healed via gap detection) rather than dropped (R9 ordering).
    if (activeSessionId) {
      ws.send(
        JSON.stringify({ type: "join", sessionId: activeSessionId }),
      );
      ws.send(
        JSON.stringify({ type: "mark_read", sessionId: activeSessionId }),
      );
      void useSessionStore.getState().fetchAgentRuns(activeSessionId);
    }

    activeSessionRef.current = activeSessionId;
  }, [activeSessionId]);

  // sendSteer delivers a drawer reply to a live agent run (feed/answer) or, if
  // the agent is idle, enqueues a new task server-side (R15/R19). The server
  // also posts the reply to the channel for shared visibility.
  const sendSteer = useCallback(
    (sessionId: string, agentId: string, message: string) => {
      const ws = wsRef.current;
      if (!ws || ws.readyState !== WebSocket.OPEN) return;
      ws.send(JSON.stringify({ type: "steer", sessionId, agentId, message }));
    },
    [],
  );

  // Register sendSteer into the store so the thread-drawer composer can steer
  // agents without re-instantiating this hook (it's mounted once in App). The
  // store forwards through store.steer(); clear the slot on unmount so a stale
  // closure over a closed socket can't linger.
  useEffect(() => {
    const setSteerSender = useSessionStore.getState().setSteerSender;
    setSteerSender(sendSteer);
    return () => setSteerSender(null);
  }, [sendSteer]);

  return { sendSteer };
}
