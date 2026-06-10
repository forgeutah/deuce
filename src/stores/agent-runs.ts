// Pure reducer for the Super Threads AgentRunEvent stream.
//
// The runtime broadcasts append-only, per-session monotonic-seq events; the
// client applies them by seq and reconciles gaps via a snapshot refetch (R9/R10,
// KTD6). This module is pure and framework-free so the ordering/dedupe logic is
// testable independent of the store and the WebSocket transport.

import type {
  AgentTask,
  AgentAction,
  TaskState,
  ActionStatus,
  TaskEventPayload,
  ActionEventPayload,
  AgentRunSnapshot,
} from "@/types";

// SessionAgentRuns is the per-session reduced state.
export interface SessionAgentRuns {
  tasks: Record<string, AgentTask>; // by task id
  lastSeq: number; // highest applied event seq
  nextOrder: number; // monotonic creation-order counter for stable sorting
}

export const emptyAgentRuns = (): SessionAgentRuns => ({ tasks: {}, lastSeq: 0, nextOrder: 0 });

// The AgentRunEvent server message types (mirror ws/events.go constants).
export const AGENT_RUN_EVENT_TYPES = [
  "task_enqueued",
  "task_started",
  "task_awaiting_input",
  "action_started",
  "action_completed",
  "task_completed",
] as const;

export type AgentRunEventType = (typeof AGENT_RUN_EVENT_TYPES)[number];

export function isAgentRunEvent(type: string): type is AgentRunEventType {
  return (AGENT_RUN_EVENT_TYPES as readonly string[]).includes(type);
}

// ApplyResult carries the new state plus whether the caller should refetch the
// snapshot because a seq gap was observed (an event was dropped — R10).
export interface ApplyResult {
  state: SessionAgentRuns;
  needsResync: boolean;
}

// applySnapshot replaces session state from a snapshot and resets the seq cursor
// to the snapshot's latestSeq. Subsequent events apply only when seq > latestSeq.
export function applySnapshot(snapshot: AgentRunSnapshot): SessionAgentRuns {
  const tasks: Record<string, AgentTask> = {};
  // Snapshot tasks arrive in creation order; assign a stable order index so the
  // drawer thread and queue positions stay chronological regardless of seq.
  snapshot.tasks.forEach((t, i) => {
    tasks[t.id] = { ...t, actions: t.actions ?? [], order: i };
  });
  return { tasks, lastSeq: snapshot.latestSeq, nextOrder: snapshot.tasks.length };
}

// applyEvent applies one AgentRunEvent. Events with seq <= lastSeq are ignored
// (duplicate or pre-snapshot). A forward gap (seq > lastSeq + 1) sets
// needsResync so the caller refetches the snapshot; the event is still applied
// so the UI keeps moving and the refetch reconciles any missed state.
export function applyEvent(
  prev: SessionAgentRuns,
  type: AgentRunEventType,
  payload: TaskEventPayload | ActionEventPayload,
): ApplyResult {
  if (payload.seq <= prev.lastSeq) {
    return { state: prev, needsResync: false };
  }
  const needsResync = prev.lastSeq > 0 && payload.seq > prev.lastSeq + 1;

  const tasks = { ...prev.tasks };
  let nextOrder = prev.nextOrder;
  switch (type) {
    case "task_enqueued":
    case "task_started":
    case "task_awaiting_input":
    case "task_completed": {
      const p = payload as TaskEventPayload;
      const existing = tasks[p.taskId];
      // Stamp a stable creation-order index the first time a task is seen, so
      // the drawer thread and queue positions stay chronological even as later
      // events overwrite task.seq.
      const order = existing?.order ?? nextOrder;
      if (existing?.order === undefined) nextOrder += 1;
      tasks[p.taskId] = { ...reduceTask(existing, type, p), order };
      break;
    }
    case "action_started":
    case "action_completed": {
      const p = payload as ActionEventPayload;
      const task = tasks[p.taskId];
      if (task) {
        tasks[p.taskId] = { ...task, actions: reduceActions(task.actions, type, p) };
      }
      break;
    }
  }

  return { state: { tasks, lastSeq: payload.seq, nextOrder }, needsResync };
}

function reduceTask(
  existing: AgentTask | undefined,
  type: AgentRunEventType,
  p: TaskEventPayload,
): AgentTask {
  const base: AgentTask =
    existing ??
    {
      id: p.taskId,
      sessionId: "",
      prompt: p.prompt ?? "",
      state: "queued",
      seq: p.seq,
      actions: [],
    };

  const next: AgentTask = { ...base, seq: p.seq };
  if (p.requestedBy !== undefined) next.requestedBy = p.requestedBy;
  if (p.anchorMessageId !== undefined) next.anchorMessageId = p.anchorMessageId;
  if (p.prompt) next.prompt = p.prompt;

  switch (type) {
    case "task_enqueued":
      next.state = "queued";
      next.position = p.position;
      break;
    case "task_started":
      next.state = "running";
      next.position = undefined;
      next.pendingQuestion = undefined;
      next.pendingQuestionKind = undefined;
      next.pendingQuestionOptions = undefined;
      break;
    case "task_awaiting_input":
      next.state = "awaiting_input";
      next.pendingQuestion = p.pendingQuestion;
      next.pendingQuestionKind = p.pendingQuestionKind;
      next.pendingQuestionOptions = p.pendingQuestionOptions;
      break;
    case "task_completed":
      next.state = (p.status as TaskState) ?? p.state ?? "done";
      next.pendingQuestion = undefined;
      next.pendingQuestionKind = undefined;
      next.pendingQuestionOptions = undefined;
      if (p.reply) next.reply = p.reply;
      break;
  }
  return next;
}

// reduceActions upserts an action by callId (dedupe so a re-attach replay or a
// snapshot/stream overlap never doubles a card — KTD13).
function reduceActions(
  actions: AgentAction[],
  type: AgentRunEventType,
  p: ActionEventPayload,
): AgentAction[] {
  const idx = actions.findIndex((a) => a.callId === p.callId);
  const status: ActionStatus =
    type === "action_completed" ? (p.isError ? "error" : "completed") : "started";

  if (idx === -1) {
    if (type === "action_completed") {
      // Completion for an action we never saw start — synthesize the row.
      return [
        ...actions,
        { callId: p.callId, seq: p.seq, tool: p.tool ?? "", arg: p.arg, text: p.text, stat: p.stat, status, isError: p.isError },
      ];
    }
    return [
      ...actions,
      { callId: p.callId, seq: p.seq, tool: p.tool ?? "", arg: p.arg, status },
    ];
  }

  const merged = [...actions];
  const cur = merged[idx];
  merged[idx] = {
    ...cur,
    seq: p.seq,
    status,
    tool: p.tool || cur.tool,
    arg: p.arg ?? cur.arg,
    text: p.text ?? cur.text,
    stat: p.stat ?? cur.stat,
    isError: type === "action_completed" ? p.isError : cur.isError,
  };
  return merged;
}

// ---------------------------------------------------------------------------
// Selectors — pure, derive view-model slices from SessionAgentRuns. These run
// on every render, so they sort/group from the task map without mutating it.
// ---------------------------------------------------------------------------

// sessionTaskList returns all tasks in stable creation order.
export function sessionTaskList(runs: SessionAgentRuns | undefined): AgentTask[] {
  if (!runs) return [];
  return Object.values(runs.tasks).sort(
    (a, b) => (a.order ?? 0) - (b.order ?? 0),
  );
}

// tasksByAnchor groups tasks under the chat message they were spawned from
// (anchorMessageId). Tasks with no anchor are omitted — they render only in the
// per-agent drawer thread, not inline beneath a message.
export function tasksByAnchor(
  runs: SessionAgentRuns | undefined,
): Record<string, AgentTask[]> {
  const out: Record<string, AgentTask[]> = {};
  for (const task of sessionTaskList(runs)) {
    if (!task.anchorMessageId) continue;
    (out[task.anchorMessageId] ??= []).push(task);
  }
  return out;
}

// queuePositions assigns a 1-based position to each queued task in creation
// order (one serial queue per session). The server sends `position` on
// enqueue, but it goes stale after promotions and is absent from snapshots —
// deriving it here keeps the "#N in queue" label correct regardless. Keyed by
// task id.
export function queuePositions(
  runs: SessionAgentRuns | undefined,
): Record<string, number> {
  const out: Record<string, number> = {};
  let n = 0;
  for (const task of sessionTaskList(runs)) {
    if (task.state !== "queued") continue;
    n += 1;
    out[task.id] = n;
  }
  return out;
}

// DeuceStatus is the derived per-session agent status for the summary panel
// and the drawer header — task state is the source of truth (the pre-013
// agent_status events are gone).
export type DeuceStatus = "idle" | "working" | "waiting";

// statusOfTasks derives the agent's status from a task list: any task
// awaiting input → "waiting"; else any running → "working"; else "idle".
// Order-independent, so no sorted copy is made.
export function statusOfTasks(tasks: Iterable<AgentTask>): DeuceStatus {
  let working = false;
  for (const task of tasks) {
    if (task.state === "awaiting_input") return "waiting";
    if (task.state === "running") working = true;
  }
  return working ? "working" : "idle";
}

// deuceStatus is statusOfTasks over a session's reduced run state.
export function deuceStatus(runs: SessionAgentRuns | undefined): DeuceStatus {
  return statusOfTasks(runs ? Object.values(runs.tasks) : []);
}

