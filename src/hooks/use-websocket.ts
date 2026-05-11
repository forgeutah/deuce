import { useEffect, useRef, useCallback } from "react";
import { useSessionStore } from "@/stores/session-store";
import type { Message, ActivityItem } from "@/types";
import { api } from "@/lib/api";

interface ServerMessage {
  type: string;
  sessionId: string;
  payload: any;
}

export function useWebSocket() {
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout>>();
  const reconnectDelay = useRef(1000);
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

  const connect = useCallback(() => {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

    ws.onopen = () => {
      console.log("[ws] connected");
      reconnectDelay.current = 1000;

      // Re-join active session
      if (activeSessionRef.current) {
        ws.send(
          JSON.stringify({
            type: "join",
            sessionId: activeSessionRef.current,
          }),
        );
      }
    };

    ws.onmessage = (event) => {
      try {
        const msg: ServerMessage = JSON.parse(event.data);
        handleMessage(msg);
      } catch {
        console.warn("[ws] failed to parse message", event.data);
      }
    };

    ws.onclose = () => {
      console.log("[ws] disconnected, reconnecting...");
      wsRef.current = null;
      reconnectTimer.current = setTimeout(() => {
        reconnectDelay.current = Math.min(reconnectDelay.current * 2, 30000);
        connect();
      }, reconnectDelay.current);
    };

    ws.onerror = (err) => {
      console.error("[ws] error", err);
      ws.close();
    };

    wsRef.current = ws;
  }, []);

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
          const { agentId, status } = msg.payload;
          updateAgentStatus(msg.sessionId, agentId, status);
          // Clear streaming output when agent finishes
          if (status === "idle" || status === "error") {
            clearAgentOutput(msg.sessionId);
          }
          break;
        }

        case "typing_indicator": {
          const { agentId, active } = msg.payload;
          if (active) {
            setThinkingAgent(msg.sessionId, agentId);
          } else {
            clearThinkingAgent(msg.sessionId, agentId);
          }
          break;
        }

        case "activity_update": {
          const activity = msg.payload as ActivityItem;
          addActivity({
            ...activity,
            sessionId: msg.sessionId || activity.sessionId,
          });
          break;
        }

        case "agent_output": {
          const { agentId, content, contentType } = msg.payload;
          appendAgentOutput(msg.sessionId, { agentId, content, contentType });
          break;
        }

        case "workspace_log": {
          const { line } = msg.payload;
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
          const { unreadCount } = msg.payload;
          const store = useSessionStore.getState();
          store.setSessions(
            store.sessions.map((s) =>
              s.id === msg.sessionId ? { ...s, unreadCount } : s,
            ),
          );
          break;
        }
      }
    },
    [addMessage, updateAgentStatus, setThinkingAgent, clearThinkingAgent, addActivity, appendWorkspaceLog, appendAgentOutput, clearAgentOutput],
  );

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

    // Join new session
    if (activeSessionId) {
      ws.send(
        JSON.stringify({ type: "join", sessionId: activeSessionId }),
      );
      ws.send(
        JSON.stringify({ type: "mark_read", sessionId: activeSessionId }),
      );
    }

    activeSessionRef.current = activeSessionId;
  }, [activeSessionId]);
}
