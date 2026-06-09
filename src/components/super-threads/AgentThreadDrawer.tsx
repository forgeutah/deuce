// AgentThreadDrawer — the per-agent global thread shown in the right panel.
// Lists every task for one agent in chronological order (the agent's whole
// history in this session), with a Claude Code-style action log per turn and a
// composer that steers the agent (or enqueues a new task when it's idle).
//
// Ported from the prototype's Drawer + Turn (queue-app.jsx), wired to real
// reducer state and the store's steer() action.

import { useState, useRef, useEffect } from "react";
import {
  X,
  SendHorizontal,
  Clock,
  ChevronRight,
  ChevronDown,
  AlertCircle,
} from "lucide-react";
import type { Agent, AgentTask, User } from "@/types";
import { AgentAvatar, TypingDots, Mentioned, ActionLog } from "./atoms";

type UserLookup = (id?: string) => Pick<User, "name" | "avatar"> | undefined;

function RequesterAvatar({ user }: { user?: Pick<User, "name" | "avatar"> }) {
  if (user?.avatar) {
    return (
      <img
        src={user.avatar}
        alt=""
        style={{ width: 24, height: 24, borderRadius: "50%", flexShrink: 0 }}
      />
    );
  }
  return (
    <div
      style={{
        width: 24,
        height: 24,
        borderRadius: "50%",
        flexShrink: 0,
        background: "var(--color-background-emphasis)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        fontSize: 11,
        fontWeight: 600,
        color: "var(--color-foreground-muted)",
      }}
    >
      {(user?.name ?? "?")[0]}
    </div>
  );
}

// QuestionControls renders the typed-prompt affordances for an awaiting-input
// task: choice buttons for a select question, yes/no for a confirm, and nothing
// extra for free text (the drawer composer below is the text input, and also
// serves as the "Other" fallback for a select). Answering routes through the
// same steer path as a typed reply (onAnswer → onSend → steer).
function QuestionControls({
  task,
  onAnswer,
}: {
  task: AgentTask;
  onAnswer: (message: string) => void;
}) {
  const kind = task.pendingQuestionKind;
  const options = task.pendingQuestionOptions ?? [];

  if (kind === "select" && options.length > 0) {
    return (
      <div className="q-choices">
        {options.map((opt) => (
          <button
            key={opt}
            type="button"
            className="q-choice"
            onClick={() => onAnswer(opt)}
          >
            {opt}
          </button>
        ))}
        <span className="q-choice-hint">or type another answer below</span>
      </div>
    );
  }

  if (kind === "confirm") {
    return (
      <div className="q-choices">
        <button type="button" className="q-choice" onClick={() => onAnswer("yes")}>
          Yes
        </button>
        <button type="button" className="q-choice" onClick={() => onAnswer("no")}>
          No
        </button>
      </div>
    );
  }

  return null;
}

function Turn({
  agent,
  task,
  queuePos,
  lookupUser,
  onAnswer,
}: {
  agent: Agent;
  task: AgentTask;
  queuePos?: number;
  lookupUser: UserLookup;
  onAnswer: (message: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const requester = lookupUser(task.requestedBy);
  const terminal =
    task.state === "done" ||
    task.state === "failed" ||
    task.state === "cancelled";

  return (
    <div className={`q-turn ${task.state}`}>
      <div className="q-req">
        <div className="hd">
          <RequesterAvatar user={requester} />
          <span className="nm">{requester?.name ?? "Someone"}</span>
        </div>
        <div className="bd">
          <Mentioned text={task.prompt} color={agent.color} />
        </div>
      </div>

      {task.state === "running" && (
        <div className="q-resp" style={{ "--ac": agent.color } as React.CSSProperties}>
          <div className="q-typingline">
            <AgentAvatar agent={agent} size={22} />
            <TypingDots color={agent.color} />
            <span className="lbl">{agent.name} is working…</span>
          </div>
          <ActionLog actions={task.actions} />
        </div>
      )}

      {task.state === "awaiting_input" && (
        <div className="q-resp" style={{ "--ac": agent.color } as React.CSSProperties}>
          <ActionLog actions={task.actions} />
          <div className="q-pending-q">
            <div className="l1">
              <AlertCircle size={12} />
              {agent.name} needs your input
            </div>
            <div className="q-text">
              {task.pendingQuestion ?? "Reply below to continue."}
            </div>
            <QuestionControls task={task} onAnswer={onAnswer} />
          </div>
        </div>
      )}

      {terminal && (
        <div className="q-resp" style={{ "--ac": agent.color } as React.CSSProperties}>
          <div className="agent-line">
            <AgentAvatar agent={agent} size={22} />
            <span className="nm">{agent.name}</span>
          </div>
          {task.actions.length > 0 && (
            <>
              <div
                className="q-actsum"
                onClick={(e) => {
                  e.stopPropagation();
                  setOpen((v) => !v);
                }}
              >
                {open ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
                <span className="ct">{task.actions.length} actions</span>
              </div>
              {open && <ActionLog actions={task.actions} />}
            </>
          )}
          <div className="bd">
            {task.reply ??
              (task.state === "failed"
                ? "Run failed."
                : task.state === "cancelled"
                  ? "Run cancelled."
                  : "Done.")}
          </div>
        </div>
      )}

      {task.state === "queued" && (
        <div className="q-queued-card">
          <AgentAvatar agent={agent} size={22} />
          <div className="info">
            <div className="l1">
              <Clock size={12} />
              Queued{queuePos != null ? ` · position ${queuePos}` : ""}
            </div>
            <div className="l2">
              Waiting for {agent.name}'s current task to finish, then starts
              automatically.
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export function AgentThreadDrawer({
  agent,
  tasks,
  queuePositions,
  lookupUser,
  onClose,
  onSend,
}: {
  agent: Agent;
  tasks: AgentTask[];
  queuePositions: Record<string, number>;
  lookupUser: UserLookup;
  onClose: () => void;
  onSend: (message: string) => void;
}) {
  const [val, setVal] = useState("");
  const bodyRef = useRef<HTMLDivElement>(null);

  const running = tasks.some((t) => t.state === "running");
  const awaiting = tasks.some((t) => t.state === "awaiting_input");

  // Keep the thread pinned to the latest activity.
  const actionSig = tasks.map((t) => `${t.state}:${t.actions.length}`).join("|");
  useEffect(() => {
    const el = bodyRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [tasks.length, actionSig]);

  const send = () => {
    const t = val.trim();
    if (!t) return;
    onSend(t);
    setVal("");
  };

  const subLabel = awaiting
    ? "Needs input · global thread"
    : running
      ? "Working · global thread"
      : "Idle · global thread";

  return (
    <div className="q-drawer" style={{ "--ac": agent.color } as React.CSSProperties}>
      <div className="q-drawer-hd">
        <AgentAvatar agent={agent} size={26} />
        <div style={{ minWidth: 0 }}>
          <div className="nm">{agent.name}</div>
          <div className="sub">
            {(running || awaiting) && (
              <span
                style={{
                  width: 7,
                  height: 7,
                  borderRadius: "50%",
                  background: agent.color,
                  display: "inline-block",
                }}
              />
            )}
            {subLabel}
          </div>
        </div>
        <button className="x" onClick={onClose} aria-label="Close thread">
          <X size={17} />
        </button>
      </div>

      <div className="q-thread" ref={bodyRef}>
        <div className="q-thread-foot" style={{ paddingTop: 0 }}>
          Start of thread with {agent.name}
        </div>
        {tasks.length === 0 ? (
          <div
            style={{
              padding: "24px 16px",
              textAlign: "center",
              fontSize: 12,
              color: "var(--color-foreground-subtle)",
            }}
          >
            No tasks yet. Send {agent.name} a message below.
          </div>
        ) : (
          tasks.map((t) => (
            <Turn
              key={t.id}
              agent={agent}
              task={t}
              queuePos={queuePositions[t.id]}
              lookupUser={lookupUser}
              onAnswer={onSend}
            />
          ))
        )}
      </div>

      <div className="q-drawer-composer">
        <div className="row">
          <textarea
            rows={1}
            value={val}
            placeholder={`Reply to ${agent.name}…`}
            onChange={(e) => setVal(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
          />
          <button className="send" onClick={send} disabled={!val.trim()} aria-label="Send">
            <SendHorizontal size={16} />
          </button>
        </div>
      </div>
    </div>
  );
}
