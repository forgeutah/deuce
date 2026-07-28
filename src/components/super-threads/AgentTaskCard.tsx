// AgentTaskCard — the inline card rendered beneath the chat message that
// spawned a deuce task (anchorMessageId). One card per task; its appearance
// switches on task.state. Clicking opens the session's thread drawer; the
// Stop button on a live card cancels the run without opening the drawer.

import {
  Loader,
  ChevronRight,
  Clock,
  Check,
  AlertCircle,
  XCircle,
} from "lucide-react";
import type { AgentTask } from "@/types";
import { DEUCE } from "@/lib/deuce";
import { AgentAvatar, StopButton, TypingDots } from "./atoms";
import { askUserQuestion, stripMention, taskFallbackMessage } from "./utils";
import { toPlainText } from "@/lib/markdown-plain";

export function AgentTaskCard({
  sessionId,
  task,
  queuePos,
  onOpen,
}: {
  sessionId: string;
  task: AgentTask;
  queuePos?: number;
  onOpen: () => void;
}) {
  const state = task.state;
  const latest = task.actions[task.actions.length - 1];
  // An in-flight ask_user call shows its question on the live line, never the
  // raw tool args (R9). Covers post-fix "Ask" rows and legacy "Ask_user" rows.
  const latestQuestion = latest ? askUserQuestion(latest.tool, latest.arg) : null;

  return (
    <div
      className={`tc ${state}`}
      style={{ "--ac": DEUCE.color } as React.CSSProperties}
      onClick={onOpen}
    >
      <div className="tc-inner">
        {state === "running" && (
          <>
            <div className="tc-hd">
              <AgentAvatar size={22} />
              <span className="nm">{DEUCE.name}</span>
              <span className="role">· session thread</span>
              <span className="spacer" />
              <span className="q-badge working">
                <Loader size={11} className="spin" />
                Working
              </span>
              <StopButton sessionId={sessionId} />
              <span className="chev">
                <ChevronRight size={15} />
              </span>
            </div>
            <div className="tc-live">
              <span className="pip" />
              <span className="tool">
                {latest?.tool === "Think"
                  ? "Thinking"
                  : latestQuestion !== null
                    ? "Asked"
                    : latest?.tool ?? "Starting"}
              </span>
              <span className="arg">
                {latest && latest.tool !== "Think"
                  ? latestQuestion ?? latest.arg
                  : ""}
              </span>
            </div>
            <div className="tc-typing">
              <TypingDots />
              <span className="lbl">{DEUCE.name} is working — open to watch</span>
            </div>
          </>
        )}

        {state === "awaiting_input" && (
          <div className="tc-q">
            <AgentAvatar size={22} />
            <div className="info">
              <div className="l1">
                <AlertCircle size={12} />
                {DEUCE.name} needs your input — open to answer
              </div>
              <div className="l2">
                {task.pendingQuestion ?? stripMention(task.prompt)}
              </div>
            </div>
            <span className="chev">
              <ChevronRight size={15} />
            </span>
          </div>
        )}

        {state === "queued" && (
          <div className="tc-q">
            <AgentAvatar size={22} />
            <div className="info">
              <div className="l1">
                <Clock size={12} />
                Queued for {DEUCE.name} · waiting for current task
              </div>
              <div className="l2">{stripMention(task.prompt)}</div>
            </div>
            {queuePos != null && <span className="pos">#{queuePos}</span>}
          </div>
        )}

        {(state === "done" || state === "failed" || state === "cancelled") && (
          <div className={`tc-d ${state}`}>
            <AgentAvatar size={18} />
            <span className="ck">
              {state === "done" ? (
                <Check size={13} />
              ) : state === "failed" ? (
                <AlertCircle size={13} />
              ) : (
                <XCircle size={13} />
              )}
            </span>
            <span className="l">
              <b>{DEUCE.name}</b>{" "}
              {task.reply ? toPlainText(task.reply) : taskFallbackMessage(state)}
            </span>
            <span className="chev" style={{ marginLeft: 2 }}>
              <ChevronRight size={14} style={{ color: "var(--color-foreground-subtle)" }} />
            </span>
          </div>
        )}
      </div>
    </div>
  );
}
