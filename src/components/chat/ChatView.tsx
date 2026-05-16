import { useState, useRef, useEffect } from "react";
import { SendHorizontal, Bot, Square } from "lucide-react";
import { Button } from "@/components/ui/button";

import { cn } from "@/lib/utils";
import { api } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";
import type { Agent, Message, User } from "@/types";
import { PatchMarker } from "@/components/chat/PatchMarker";

function TypingIndicator({
  agentName,
  agentColor,
  sessionId,
  streamingOutput,
}: {
  agentName: string;
  agentColor: string;
  sessionId: string;
  streamingOutput: { content: string; contentType: string }[];
}) {
  const streamEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    streamEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [streamingOutput.length]);

  const handleStop = async () => {
    try {
      await api.stopAgent(sessionId);
    } catch (err) {
      console.error("Failed to stop agent:", err);
    }
  };

  return (
    <div className="px-4 py-2 animate-fade-in-up">
      <div className="flex items-center gap-2">
        <div
          className="flex h-7 w-7 items-center justify-center rounded text-xs font-semibold text-foreground-on-emphasis shrink-0"
          style={{ backgroundColor: agentColor }}
        >
          {agentName[0]}
        </div>
        <span className="text-sm text-foreground-muted">{agentName} is working</span>
        <div className="flex gap-1">
          <span className="h-1.5 w-1.5 rounded-full bg-foreground-muted animate-typing-dot" style={{ animationDelay: "0s" }} />
          <span className="h-1.5 w-1.5 rounded-full bg-foreground-muted animate-typing-dot" style={{ animationDelay: "0.2s" }} />
          <span className="h-1.5 w-1.5 rounded-full bg-foreground-muted animate-typing-dot" style={{ animationDelay: "0.4s" }} />
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={handleStop}
          className="ml-auto h-6 w-6 text-danger hover:text-danger hover:bg-danger/10"
          title="Stop agent"
        >
          <Square className="h-3 w-3 fill-current" />
        </Button>
      </div>

      {/* Streaming output */}
      {streamingOutput.length > 0 && (
        <div className="mt-2 ml-9 max-h-32 overflow-y-auto rounded-md border border-border-muted bg-background-subtle p-2">
          {streamingOutput.map((item, i) => (
            <span
              key={i}
              className={cn(
                "text-xs",
                item.contentType === "tool_use"
                  ? "text-accent font-medium"
                  : "text-foreground-muted",
              )}
            >
              {item.contentType === "tool_use" ? `[${item.content}] ` : item.content}
            </span>
          ))}
          <div ref={streamEndRef} />
        </div>
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
  author: Agent | User | undefined;
  showHeader: boolean;
}) {
  const [expandedItems, setExpandedItems] = useState<Set<number>>(new Set());
  const isAgent = message.authorType === "agent";
  const agentAuthor = isAgent ? (author as Agent) : undefined;

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
        isAgent && "border-l-2 bg-opacity-5",
      )}
      style={
        isAgent && agentAuthor
          ? {
              borderLeftColor: agentAuthor.color,
              backgroundColor: `${agentAuthor.color}08`,
            }
          : undefined
      }
    >
      {showHeader && (
        <div className="flex items-center gap-2 mb-0.5">
          {isAgent && agentAuthor ? (
            <div
              className="flex h-7 w-7 items-center justify-center rounded text-xs font-semibold text-foreground-on-emphasis shrink-0"
              style={{ backgroundColor: agentAuthor.color }}
            >
              {agentAuthor.name[0]}
            </div>
          ) : (
            <img
              src={(author as User)?.avatar ?? ""}
              alt=""
              className="h-7 w-7 rounded-full shrink-0"
            />
          )}
          <span className="text-sm font-semibold text-foreground-emphasis">
            {isAgent ? agentAuthor?.name : (author as User)?.name}
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
  const { activeSessionId, sessions, messages, patches, thinkingAgents, agentOutput, addMessage } =
    useSessionStore();
  const [input, setInput] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);
  const isNearBottomRef = useRef(true);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const session = sessions.find((s) => s.id === activeSessionId);
  const sessionMessages = activeSessionId
    ? (messages[activeSessionId] ?? [])
    : [];
  const sessionPatches = activeSessionId
    ? (patches[activeSessionId] ?? [])
    : [];
  // Index patches by producing_message_id so each message can render its
  // patches inline. v0 producers always set this, so orphan patches (null
  // producingMessageId) are not rendered — see plan U8 and SG4 finding.
  // Multiple patches per message are stacked in createdAt order.
  const patchesByMessage = sessionPatches.reduce<Record<string, typeof sessionPatches>>(
    (acc, p) => {
      if (!p.producingMessageId) return acc;
      (acc[p.producingMessageId] ??= []).push(p);
      return acc;
    },
    {},
  );
  const thinking = activeSessionId
    ? (thinkingAgents[activeSessionId] ?? [])
    : [];
  const streamOutput = activeSessionId
    ? (agentOutput[activeSessionId] ?? [])
    : [];

  const allParticipants = session
    ? [...session.agents, ...session.members]
    : [];

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
  }, [sessionMessages.length, thinking.length]);

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

    // Detect @mentions — match agent names to IDs
    const mentionMatch = content.match(/@(\w+)/g);
    const mentions: string[] = [];
    if (mentionMatch) {
      for (const mention of mentionMatch) {
        const agentName = mention.slice(1);
        const agent = session.agents.find(
          (a) => a.name.toLowerCase() === agentName.toLowerCase(),
        );
        if (agent) mentions.push(agent.id);
      }
    }

    try {
      // POST to API — server handles persistence, broadcasting, and agent responses
      const msg = await api.sendMessage(activeSessionId, { content, mentions });
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

  return (
    <div className="flex h-full flex-col">
      {/* Messages */}
      <div className="flex-1 overflow-y-auto" onScroll={handleScroll}>
        <div className="flex flex-col gap-1 py-4">
          {sessionMessages.length === 0 && (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <Bot className="h-10 w-10 text-foreground-subtle mb-3" />
              <h3 className="text-sm font-medium text-foreground-muted">
                Start a conversation
              </h3>
              <p className="mt-1 text-xs text-foreground-subtle max-w-xs">
                @mention an agent to get started.
                {session?.agents.map((a) => (
                  <button
                    key={a.id}
                    onClick={() => setInput(`@${a.name} `)}
                    className="mx-1 rounded-full px-2 py-0.5 text-xs font-medium text-foreground-on-emphasis"
                    style={{ backgroundColor: a.color }}
                  >
                    @{a.name}
                  </button>
                ))}
              </p>
            </div>
          )}

          {sessionMessages.map((msg, i) => {
            const prevMsg = sessionMessages[i - 1];
            const showHeader =
              !prevMsg ||
              prevMsg.authorId !== msg.authorId ||
              new Date(msg.createdAt).getTime() -
                new Date(prevMsg.createdAt).getTime() >
                300000;

            const author =
              msg.authorType === "agent"
                ? session?.agents.find((a) => a.id === msg.authorId)
                : allParticipants.find(
                    (p) => "email" in p && p.id === msg.authorId,
                  ) ?? session?.members.find((m) => m.id === msg.authorId);

            const messagePatches = patchesByMessage[msg.id] ?? [];

            return (
              <div key={msg.id}>
                <MessageBubble
                  message={msg}
                  author={author}
                  showHeader={showHeader}
                />
                {messagePatches.map((patch) => (
                  <PatchMarker
                    key={patch.id}
                    patch={patch}
                    producingAgent={
                      msg.authorType === "agent"
                        ? (author as Agent | undefined)
                        : undefined
                    }
                  />
                ))}
              </div>
            );
          })}

          {/* Typing indicators with streaming output */}
          {thinking.map((agentId) => {
            const agent = session?.agents.find((a) => a.id === agentId);
            if (!agent) return null;
            const agentStream = streamOutput.filter((o) => o.agentId === agentId);
            return (
              <TypingIndicator
                key={agentId}
                agentName={agent.name}
                agentColor={agent.color}
                sessionId={activeSessionId!}
                streamingOutput={agentStream}
              />
            );
          })}

          <div ref={bottomRef} />
        </div>
      </div>

      {/* Input */}
      <div className="border-t border-border-muted p-3">
        {isReadOnly ? (
          <div className="flex items-center justify-center rounded-md border border-border-muted bg-background-subtle py-2 text-xs text-foreground-subtle">
            {session?.status === "paused"
              ? "Session is paused"
              : "Session is archived"}
          </div>
        ) : (
          <div className="flex items-end gap-2">
            <textarea
              ref={inputRef}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder="Message (@ to mention an agent)"
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

