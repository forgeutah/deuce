import type { Message } from "@/types";
import { SYSTEM_AUTHOR_ID } from "@/lib/deuce";

// Deuce's task replies are surfaced on the super-thread surfaces (the inline
// AgentTaskCard and the thread drawer), so their chat-bubble copy is hidden
// from the main chat list. System/operational notices are posted with
// authorType "agent" but the nil author ID (postSystemMessage in
// server/internal/handler/messages.go) — they stay visible, as do all human
// messages. Agent-typed messages with any other author should not exist
// post-migration (013 repoints history to DEUCE.id), but hide them too so an
// unexpected author can't leak a duplicate reply into chat.
export function isVisibleInChat(message: Message): boolean {
  return !(
    message.authorType === "agent" && message.authorId !== SYSTEM_AUTHOR_ID
  );
}

// visibleChatMessages filters a message list for the main chat surface,
// preserving order and never mutating the input.
export function visibleChatMessages(messages: Message[]): Message[] {
  return messages.filter(isVisibleInChat);
}
