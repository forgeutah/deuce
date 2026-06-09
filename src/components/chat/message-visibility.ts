import type { Message } from "@/types";

// Agent task replies are surfaced on the super-thread surfaces (the inline
// AgentTaskCard and the agent thread drawer), so their chat-bubble copy is
// hidden from the main chat list. System/operational notices are posted with
// authorType "agent" but a nil author ID that matches no session agent
// (postSystemMessage in server/internal/handler/messages.go) — they fall
// through the agentIds check and stay visible, as do all human messages.
export function isVisibleInChat(
  message: Message,
  agentIds: Set<string>,
): boolean {
  return !(message.authorType === "agent" && agentIds.has(message.authorId));
}

// visibleChatMessages filters a message list for the main chat surface,
// preserving order and never mutating the input. agentIds is the set of the
// session's agent IDs (derived from session.agents by the caller — this
// module stays free of store/session imports).
export function visibleChatMessages(
  messages: Message[],
  agentIds: Set<string>,
): Message[] {
  return messages.filter((m) => isVisibleInChat(m, agentIds));
}
