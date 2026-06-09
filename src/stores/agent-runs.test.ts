import { describe, expect, it } from "vitest";

import { applyEvent, applySnapshot } from "./agent-runs";
import type { AgentRunSnapshot, TaskEventPayload } from "@/types";

// NOTE: The frontend has no test runner wired up yet (see CLAUDE.md / repo.test.ts).
// These Vitest-style specs capture the intended behavior and run as soon as a
// runner is added. Until then, `npx tsc --noEmit` keeps them type-checked.

const empty = { tasks: {}, lastSeq: 0, nextOrder: 0 };

function awaitingEvent(extra: Partial<TaskEventPayload>): TaskEventPayload {
  return {
    seq: 1,
    taskId: "t1",
    agentId: "a1",
    pendingQuestion: "Which framework?",
    ...extra,
  };
}

describe("agent-runs reducer — typed questions", () => {
  it("carries kind + options on task_awaiting_input (select)", () => {
    const { state } = applyEvent(
      empty,
      "task_awaiting_input",
      awaitingEvent({
        pendingQuestionKind: "select",
        pendingQuestionOptions: ["React", "Vue", "Svelte"],
      }),
    );
    const task = state.tasks["t1"];
    expect(task.state).toBe("awaiting_input");
    expect(task.pendingQuestionKind).toBe("select");
    expect(task.pendingQuestionOptions).toEqual(["React", "Vue", "Svelte"]);
  });

  it("is backward-compatible: no kind reduces to free text", () => {
    const { state } = applyEvent(empty, "task_awaiting_input", awaitingEvent({}));
    const task = state.tasks["t1"];
    expect(task.state).toBe("awaiting_input");
    expect(task.pendingQuestion).toBe("Which framework?");
    expect(task.pendingQuestionKind).toBeUndefined();
    expect(task.pendingQuestionOptions).toBeUndefined();
  });

  it("clears kind + options when the task resumes (task_started)", () => {
    const afterAsk = applyEvent(
      empty,
      "task_awaiting_input",
      awaitingEvent({
        pendingQuestionKind: "confirm",
      }),
    ).state;
    const { state } = applyEvent(afterAsk, "task_started", {
      seq: 2,
      taskId: "t1",
      agentId: "a1",
    });
    const task = state.tasks["t1"];
    expect(task.state).toBe("running");
    expect(task.pendingQuestion).toBeUndefined();
    expect(task.pendingQuestionKind).toBeUndefined();
    expect(task.pendingQuestionOptions).toBeUndefined();
  });

  it("preserves kind + options through a snapshot (reconnect / seq-gap refetch)", () => {
    const snapshot: AgentRunSnapshot = {
      tasks: [
        {
          id: "t1",
          sessionId: "s1",
          agentId: "a1",
          prompt: "@coder build it",
          state: "awaiting_input",
          seq: 5,
          pendingQuestion: "Which framework?",
          pendingQuestionKind: "select",
          pendingQuestionOptions: ["React", "Vue"],
          actions: [],
        },
      ],
      latestSeq: 5,
    };
    const state = applySnapshot(snapshot);
    const task = state.tasks["t1"];
    expect(task.pendingQuestionKind).toBe("select");
    expect(task.pendingQuestionOptions).toEqual(["React", "Vue"]);
  });
});
