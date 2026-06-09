import { describe, expect, it } from "vitest";
import { isVisibleInChat, visibleChatMessages } from "./message-visibility";
import type { Message } from "@/types";

// NOTE: The frontend has no test runner wired up yet (see CLAUDE.md). These
// Vitest-style specs capture the intended behavior and run as soon as a runner
// is added. Until then, `npx tsc --noEmit` keeps them type-checked.

const NIL_UUID = "00000000-0000-0000-0000-000000000000";
const AGENT_ID = "a1111111-1111-1111-1111-111111111111";
const USER_ID = "u1111111-1111-1111-1111-111111111111";

function msg(overrides: Partial<Message>): Message {
  return {
    id: "m1",
    sessionId: "s1",
    authorId: USER_ID,
    authorType: "human",
    content: "hello",
    mentions: [],
    createdAt: "2026-06-08T12:00:00Z",
    status: "sent",
    ...overrides,
  };
}

describe("isVisibleInChat", () => {
  it("keeps human messages visible regardless of agentIds", () => {
    const m = msg({ authorType: "human", authorId: USER_ID });
    expect(isVisibleInChat(m, new Set())).toBe(true);
    expect(isVisibleInChat(m, new Set([AGENT_ID, USER_ID]))).toBe(true);
  });

  it("hides an agent reply whose author is a session agent", () => {
    const m = msg({ authorType: "agent", authorId: AGENT_ID });
    expect(isVisibleInChat(m, new Set([AGENT_ID]))).toBe(false);
  });

  it("keeps system notices visible (agent-typed, nil author ID)", () => {
    const m = msg({ authorType: "agent", authorId: NIL_UUID });
    expect(isVisibleInChat(m, new Set([AGENT_ID]))).toBe(true);
  });

  it("keeps agent-typed messages visible when the author is not a session agent", () => {
    // Defensive: e.g. the agent was removed from the session.
    const m = msg({ authorType: "agent", authorId: "gone-agent" });
    expect(isVisibleInChat(m, new Set([AGENT_ID]))).toBe(true);
  });

  it("hides nothing when agentIds is empty", () => {
    const m = msg({ authorType: "agent", authorId: AGENT_ID });
    expect(isVisibleInChat(m, new Set())).toBe(true);
  });
});

describe("visibleChatMessages", () => {
  it("returns only visible messages in original order without mutating input", () => {
    const human1 = msg({ id: "m1", authorType: "human", authorId: USER_ID });
    const reply = msg({ id: "m2", authorType: "agent", authorId: AGENT_ID });
    const notice = msg({ id: "m3", authorType: "agent", authorId: NIL_UUID });
    const human2 = msg({ id: "m4", authorType: "human", authorId: USER_ID });
    const input = [human1, reply, notice, human2];

    const out = visibleChatMessages(input, new Set([AGENT_ID]));

    expect(out).toEqual([human1, notice, human2]);
    expect(input).toHaveLength(4);
    expect(input[1]).toBe(reply);
  });

  it("returns everything when agentIds is empty", () => {
    const input = [
      msg({ id: "m1", authorType: "agent", authorId: AGENT_ID }),
      msg({ id: "m2", authorType: "human", authorId: USER_ID }),
    ];
    expect(visibleChatMessages(input, new Set())).toEqual(input);
  });
});
