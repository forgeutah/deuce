import { describe, expect, it } from "vitest";

import {
  applyEvent,
  applySnapshot,
  deuceStatus,
  queuePositions,
} from "./agent-runs";
import type { AgentRunSnapshot, TaskEventPayload } from "@/types";

const empty = { tasks: {}, lastSeq: 0, nextOrder: 0 };

function awaitingEvent(extra: Partial<TaskEventPayload>): TaskEventPayload {
  return {
    seq: 1,
    taskId: "t1",
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
          prompt: "@deuce build it",
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

describe("deuceStatus / queuePositions — single-session derivations", () => {
  const seed = (states: ("queued" | "running" | "awaiting_input" | "done")[]) => {
    let state = { tasks: {}, lastSeq: 0, nextOrder: 0 } as ReturnType<
      typeof applySnapshot
    >;
    states.forEach((s, i) => {
      state = applyEvent(state, "task_enqueued", {
        seq: i * 2 + 1,
        taskId: `t${i}`,
        prompt: `p${i}`,
      }).state;
      if (s === "running" || s === "awaiting_input" || s === "done") {
        state = applyEvent(state, "task_started", {
          seq: i * 2 + 2,
          taskId: `t${i}`,
        }).state;
      }
    });
    return state;
  };

  it("derives idle / working / waiting from task state", () => {
    expect(deuceStatus(undefined)).toBe("idle");
    expect(deuceStatus(seed(["queued"]))).toBe("idle");
    expect(deuceStatus(seed(["running"]))).toBe("working");
    const waiting = applyEvent(seed(["running"]), "task_awaiting_input", {
      seq: 99,
      taskId: "t0",
      pendingQuestion: "?",
    }).state;
    expect(deuceStatus(waiting)).toBe("waiting");
  });

  it("numbers the session's queued tasks in creation order", () => {
    const state = seed(["running", "queued", "queued"]);
    expect(queuePositions(state)).toEqual({ t1: 1, t2: 2 });
  });
});
