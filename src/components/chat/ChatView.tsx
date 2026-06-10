import { useState, useRef, useEffect } from "react";
import {
  SendHorizontal,
  Bot,
  Play,
  RefreshCw,
  AlertCircle,
  Loader2,
  UserPlus,
} from "lucide-react";
import { Button } from "@/components/ui/button";

import { cn } from "@/lib/utils";
import { api, ApiError } from "@/lib/api";
import { isSessionMember } from "@/lib/membership";
import { useSessionStore } from "@/stores/session-store";
import { tasksByAnchor, queuePositions } from "@/stores/agent-runs";
import { AgentTaskCard } from "@/components/super-threads/AgentTaskCard";
import { visibleChatMessages } from "@/components/chat/message-visibility";
import { DEUCE } from "@/lib/deuce";
import type { Message, User, Session, WorkspaceStatus } from "@/types";

function isWorkspaceLive(status: WorkspaceStatus | undefined): boolean {
  return status === "ready" || status === "starting";
}

function isWorkspaceTransitioning(status: WorkspaceStatus | undefined): boolean {
  return (
    status === "starting" ||
    status === "stopping" ||
    status === "rebuilding" ||
    status === "deleting"
  );
}

const NON_LIVE_MESSAGES: Partial<Record<WorkspaceStatus, string>> = {
  stopped: "Workspace is stopped",
  missing: "Workspace no longer exists",
  failed: "Workspace failed to start",
  stopping: "Stopping workspace…",
  rebuilding: "Rebuilding workspace…",
  deleting: "Deleting workspace…",
};

// WorkspaceComposerGate replaces the chat composer when the workspace isn't
// live. Messages above stay scrollable and readable (just dimmed) so users
// can review history before deciding what to do; the composer is locked
// because sending a message would target an agent that has no container to
// run in.
function WorkspaceComposerGate({ session }: { session: Session }) {
  const updateWorkspaceStatus = useSessionStore((s) => s.updateWorkspaceStatus);
  const [pending, setPending] = useState<"start" | "rebuild" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const status = session.workspaceStatus;
  const transitioning = isWorkspaceTransitioning(status);
  const message = NON_LIVE_MESSAGES[status] ?? "Workspace not available";

  // 'stopped' → Start (cheap, resumes the existing container).
  // 'missing' / 'failed' → Rebuild (creates a fresh container).
  // Transitional → no action; the spinner waits for the server to settle.
  const action: "start" | "rebuild" | null =
    status === "stopped"
      ? "start"
      : status === "missing" || status === "failed"
        ? "rebuild"
        : null;

  async function fire() {
    if (!action || pending) return;
    setError(null);
    setPending(action);
    try {
      const fn =
        action === "start" ? api.startWorkspace : api.rebuildWorkspace;
      const updated = await fn(session.id);
      updateWorkspaceStatus(session.id, updated.workspaceStatus);
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : `${action} failed. Try again.`,
      );
    } finally {
      setPending(null);
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-muted bg-background-subtle px-3 py-2.5">
      <div className="flex items-center gap-2 text-sm">
        {transitioning ? (
          <Loader2 className="h-4 w-4 shrink-0 animate-spin text-warning" />
        ) : (
          <AlertCircle className="h-4 w-4 shrink-0 text-danger" />
        )}
        <span className="text-foreground-muted">{message}</span>
        {action && !transitioning && (
          <Button
            onClick={fire}
            disabled={pending !== null}
            size="sm"
            className="ml-auto gap-1.5 h-7"
          >
            {pending === action ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : action === "start" ? (
              <Play className="h-3.5 w-3.5" />
            ) : (
              <RefreshCw className="h-3.5 w-3.5" />
            )}
            {action === "start" ? "Start workspace" : "Rebuild workspace"}
          </Button>
        )}
      </div>
      {error && (
        <p className="text-xs text-danger" role="alert">
          {error}
        </p>
      )}
      {!error && (
        <p className="text-[11px] text-foreground-subtle">
          History stays readable. Sending a new message is locked until the
          workspace is back up.
        </p>
      )}
    </div>
  );
}

// JoinSessionGate replaces the composer when the current user can see and read
// the session (their team owns it) but has not joined it. Reading is allowed
// (history stays fully readable above); posting and steering require
// membership, so the primary action here is to Join. This gate takes priority
// over the workspace gate — there's no point offering Start/Rebuild to someone
// who hasn't joined yet.
function JoinSessionGate({ session }: { session: Session }) {
  const joinSession = useSessionStore((s) => s.joinSession);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function join() {
    if (pending) return;
    setError(null);
    setPending(true);
    try {
      await joinSession(session.id);
      // On success the store flips membership and this gate unmounts.
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Couldn't join. Try again.",
      );
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-muted bg-background-subtle px-3 py-2.5">
      <div className="flex items-center gap-2 text-sm">
        <span className="text-foreground-muted">
          You're viewing{" "}
          <span className="font-medium text-foreground">#{session.name}</span>.
          Join to send messages and run agents.
        </span>
        <Button
          onClick={join}
          disabled={pending}
          size="sm"
          className="ml-auto gap-1.5 h-7"
        >
          {pending ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <UserPlus className="h-3.5 w-3.5" />
          )}
          Join Session
        </Button>
      </div>
      {error && (
        <p className="text-xs text-danger" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

function MessageBubble({
  message,
  author,
  showHeader,
}: {
  message: Message;
  author: User | undefined;
  showHeader: boolean;
}) {
  const [expandedItems, setExpandedItems] = useState<Set<number>>(new Set());
  // The only agent-typed messages that pass the visibility filter are system
  // notices (nil author) — deuce's task replies render on the task cards.
  const isSystem = message.authorType === "agent";

  const toggleExpand = (index: number) => {
    setExpandedItems((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  };

  return (
    <div
      className={cn(
        "group px-4 py-1 animate-fade-in-up",
        isSystem && "border-l-2 bg-opacity-5",
      )}
      style={
        isSystem
          ? {
              borderLeftColor: DEUCE.colorMuted,
              backgroundColor: `${DEUCE.color}08`,
            }
          : undefined
      }
    >
      {showHeader && (
        <div className="flex items-center gap-2 mb-0.5">
          {isSystem ? (
            <div className="flex h-7 w-7 items-center justify-center rounded bg-background-emphasis text-foreground-muted shrink-0">
              <Bot className="h-4 w-4" />
            </div>
          ) : (
            <img
              src={author?.avatar ?? ""}
              alt=""
              className="h-7 w-7 rounded-full shrink-0"
            />
          )}
          <span className="text-sm font-semibold text-foreground-emphasis">
            {isSystem ? "system" : author?.name}
          </span>
          <span className="text-xs text-foreground-subtle">
            {new Date(message.createdAt).toLocaleTimeString([], {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </span>
        </div>
      )}
      <div className={cn("text-sm text-foreground", showHeader && "pl-9")}>
        <p className="whitespace-pre-wrap">{message.content}</p>

        {/* Expandable content */}
        {message.expandableContent?.map((item, i) => (
          <div key={i} className="mt-2">
            <button
              onClick={() => toggleExpand(i)}
              className="text-xs text-accent hover:underline"
            >
              {expandedItems.has(i) ? "Hide" : "Show"} {item.title}
            </button>
            {expandedItems.has(i) && (
              <div className="mt-1 rounded-md border border-border bg-background-subtle p-3 font-mono text-xs animate-fade-in-up">
                <div className="mb-1 text-foreground-muted">{item.summary}</div>
                <pre className="overflow-x-auto whitespace-pre text-foreground">
                  {item.content}
                </pre>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

export function ChatView() {
  const {
    activeSessionId,
    sessions,
    messages,
    addMessage,
    agentRuns,
    openAgentThread,
    currentUser,
  } = useSessionStore();
  const [input, setInput] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);
  const isNearBottomRef = useRef(true);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const session = sessions.find((s) => s.id === activeSessionId);
  const sessionMessages = activeSessionId
    ? (messages[activeSessionId] ?? [])
    : [];

  // Deuce's task replies render on the super-thread surfaces (inline card +
  // drawer), not as chat bubbles. System notices (agent-typed, nil author)
  // stay visible.
  const visibleMessages = visibleChatMessages(sessionMessages);

  // Super Threads: inline task cards anchored to the message that spawned them.
  const sessionRuns = activeSessionId ? agentRuns[activeSessionId] : undefined;
  const cardsByAnchor = tasksByAnchor(sessionRuns);
  const queuePos = queuePositions(sessionRuns);

  // Track whether user is near the bottom of the scroll area
  const handleScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const threshold = 100;
    isNearBottomRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < threshold;
  };

  // Auto-scroll only when user is near bottom, or on session switch
  useEffect(() => {
    if (isNearBottomRef.current) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [sessionMessages.length]);

  // Always scroll to bottom on session switch
  useEffect(() => {
    isNearBottomRef.current = true;
    bottomRef.current?.scrollIntoView();
  }, [activeSessionId]);

  const handleSend = async () => {
    if (!input.trim() || !activeSessionId || !session) return;
    if (session.status !== "active") return;

    const content = input.trim();
    setInput("");

    try {
      // POST to API — the server persists, broadcasts, and detects the @deuce
      // mention itself (no client-side mention parsing).
      const msg = await api.sendMessage(activeSessionId, { content });
      // Add our own message locally (server broadcasts to OTHER clients)
      addMessage(msg);
    } catch (err) {
      console.error("Failed to send message:", err);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const isReadOnly = session?.status !== "active";
  const workspaceLive = isWorkspaceLive(session?.workspaceStatus);
  // Team membership grants read access; SESSION membership grants posting.
  // A viewer who hasn't joined sees the JoinSessionGate instead of a composer.
  const isMember = !!session && isSessionMember(session, currentUser?.id);

  return (
    <div className="flex h-full flex-col">
      {/* Messages — dimmed when the workspace is off, so history stays
          readable and the composer gate at the bottom is the clear next
          surface for the user. */}
      <div
        className={cn(
          "flex-1 overflow-y-auto transition-opacity",
          !workspaceLive && "opacity-60",
        )}
        onScroll={handleScroll}
      >
        <div className="flex flex-col gap-1 py-4">
          {visibleMessages.length === 0 && (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <Bot className="h-10 w-10 text-foreground-subtle mb-3" />
              <h3 className="text-sm font-medium text-foreground-muted">
                Start a conversation
              </h3>
              <p className="mt-1 text-xs text-foreground-subtle max-w-xs">
                Mention
                {isMember && workspaceLive ? (
                  <button
                    onClick={() => {
                      setInput(`@${DEUCE.name} `);
                      inputRef.current?.focus();
                    }}
                    className="mx-1 rounded-full px-2 py-0.5 text-xs font-medium text-foreground-on-emphasis"
                    style={{ backgroundColor: DEUCE.color }}
                  >
                    @{DEUCE.name}
                  </button>
                ) : (
                  <span className="mx-1 font-medium">@{DEUCE.name}</span>
                )}
                to bring the agent into the conversation.
              </p>
            </div>
          )}

          {visibleMessages.map((msg, i) => {
            const prevMsg = visibleMessages[i - 1];
            const showHeader =
              !prevMsg ||
              prevMsg.authorId !== msg.authorId ||
              new Date(msg.createdAt).getTime() -
                new Date(prevMsg.createdAt).getTime() >
                300000;

            const author =
              msg.authorType === "human"
                ? (session?.members.find((m) => m.id === msg.authorId) ??
                  (currentUser?.id === msg.authorId ? currentUser : undefined))
                : undefined;

            const anchoredTasks = cardsByAnchor[msg.id] ?? [];

            return (
              <div key={msg.id}>
                <MessageBubble
                  message={msg}
                  author={author}
                  showHeader={showHeader}
                />
                {anchoredTasks.map((task) => (
                  <AgentTaskCard
                    key={task.id}
                    sessionId={activeSessionId!}
                    task={task}
                    queuePos={queuePos[task.id]}
                    onOpen={() =>
                      activeSessionId && openAgentThread(activeSessionId)
                    }
                  />
                ))}
              </div>
            );
          })}

          <div ref={bottomRef} />
        </div>
      </div>

      {/* Input — four modes, in priority order:
            1. Session paused/archived → existing read-only banner (session
               lifecycle, not workspace lifecycle); applies to everyone.
            2. Not a session member → JoinSessionGate (primary CTA for a
               team member who is only viewing).
            3. Workspace not live → WorkspaceComposerGate with Start/Rebuild.
            4. Normal composer. */}
      <div className="border-t border-border-muted p-3">
        {isReadOnly ? (
          <div className="flex items-center justify-center rounded-md border border-border-muted bg-background-subtle py-2 text-xs text-foreground-subtle">
            {session?.status === "paused"
              ? "Session is paused"
              : "Session is archived"}
          </div>
        ) : !isMember && session ? (
          <JoinSessionGate session={session} />
        ) : !workspaceLive && session ? (
          <WorkspaceComposerGate session={session} />
        ) : (
          <div className="flex items-end gap-2">
            <textarea
              ref={inputRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Message (@deuce to bring in the agent)"
              rows={1}
              className="flex-1 resize-none rounded-md border border-border-muted bg-background-input px-3 py-2 text-sm text-foreground placeholder:text-foreground-subtle focus:border-accent focus:outline-none"
            />
            <Button
              onClick={handleSend}
              disabled={!input.trim()}
              size="icon"
              className="h-9 w-9 bg-accent-emphasis hover:bg-accent text-foreground-on-emphasis"
            >
              <SendHorizontal className="h-4 w-4" />
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}

