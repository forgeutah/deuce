import { describe, expect, it } from "vitest";
import { isVisibleInChat, visibleChatMessages } from "./message-visibility";
import { DEUCE, SYSTEM_AUTHOR_ID } from "@/lib/deuce";
import type { Message } from "@/types";

const USER_ID = "u1111111-1111-1111-1111-111111111111";

function msg(overrides: Partial<Message>): Message {
  return {
    id: "m1",
    sessionId: "s1",
    authorId: USER_ID,
    authorType: "human",
    content: "hello",
    createdAt: "2026-06-08T12:00:00Z",
    status: "sent",
    ...overrides,
  };
}

describe("isVisibleInChat", () => {
  it("keeps human messages visible", () => {
    expect(isVisibleInChat(msg({ authorType: "human" }))).toBe(true);
  });

  it("hides deuce's task replies", () => {
    const m = msg({ authorType: "agent", authorId: DEUCE.id });
    expect(isVisibleInChat(m)).toBe(false);
  });

  it("keeps system notices visible (agent-typed, nil author ID)", () => {
    const m = msg({ authorType: "agent", authorId: SYSTEM_AUTHOR_ID });
    expect(isVisibleInChat(m)).toBe(true);
  });

  it("hides agent-typed messages with an unexpected legacy author", () => {
    // Post-migration these shouldn't exist (013 repoints history to DEUCE.id);
    // hiding is the safe shape — an unknown agent author must not surface a
    // duplicate reply in chat.
    const m = msg({ authorType: "agent", authorId: "gone-agent" });
    expect(isVisibleInChat(m)).toBe(false);
  });
});


describe("visibleChatMessages", () => {
  it("returns only visible messages in original order without mutating input", () => {
    const human1 = msg({ id: "m1", authorType: "human", authorId: USER_ID });
    const reply = msg({ id: "m2", authorType: "agent", authorId: DEUCE.id });
    const notice = msg({
      id: "m3",
      authorType: "agent",
      authorId: SYSTEM_AUTHOR_ID,
    });
    const human2 = msg({ id: "m4", authorType: "human", authorId: USER_ID });
    const input = [human1, reply, notice, human2];

    const out = visibleChatMessages(input);

    expect(out).toEqual([human1, notice, human2]);
    expect(input).toHaveLength(4);
    expect(input[1]).toBe(reply);
  });
});
