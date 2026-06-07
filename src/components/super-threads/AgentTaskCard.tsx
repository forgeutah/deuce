// AgentTaskCard — the inline card rendered beneath the chat message that
// spawned an agent task (anchorMessageId). One card per task; its appearance
// switches on task.state. Clicking opens the agent's thread drawer.
//
// Ported from the prototype's TaskCard (queue-app.jsx), wired to real
// AgentTask/AgentAction reducer state instead of the demo's timer simulation.

import {
  Loader,
  ChevronRight,
  Clock,
  Check,
  AlertCircle,
  XCircle,
} from "lucide-react";
import type { Agent, AgentTask } from "@/types";
import { AgentAvatar, TypingDots } from "./atoms";
import { stripMention } from "./utils";

export function AgentTaskCard({
  agent,
  task,
  queuePos,
  onOpen,
}: {
  agent: Agent;
  task: AgentTask;
  queuePos?: number;
  onOpen: () => void;
}) {
  const state = task.state;
  const latest = task.actions[task.actions.length - 1];

  return (
    <div className={`tc ${state}`} style={{ "--ac": agent.color } as React.CSSProperties} onClick={onOpen}>
      <div className="tc-inner">
        {state === "running" && (
          <>
            <div className="tc-hd">
              <AgentAvatar agent={agent} size={22} />
              <span className="nm">{agent.name}</span>
              <span className="role">· session thread</span>
              <span className="spacer" />
              <span className="q-badge working">
                <Loader size={11} className="spin" />
                Working
              </span>
              <span className="chev">
                <ChevronRight size={15} />
              </span>
            </div>
            <div className="tc-live">
              <span className="pip" />
              <span className="tool">
                {latest?.tool === "Think" ? "Thinking" : latest?.tool ?? "Starting"}
              </span>
              <span className="arg">
                {latest && latest.tool !== "Think" ? latest.arg : ""}
              </span>
            </div>
            <div className="tc-typing">
              <TypingDots color={agent.color} />
              <span className="lbl">{agent.name} is working — open to watch</span>
            </div>
          </>
        )}

        {state === "awaiting_input" && (
          <div className="tc-q">
            <AgentAvatar agent={agent} size={22} />
            <div className="info">
              <div className="l1">
                <AlertCircle size={12} />
                {agent.name} needs your input — open to answer
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
            <AgentAvatar agent={agent} size={22} />
            <div className="info">
              <div className="l1">
                <Clock size={12} />
                Queued for {agent.name} · waiting for current task
              </div>
              <div className="l2">{stripMention(task.prompt)}</div>
            </div>
            {queuePos != null && <span className="pos">#{queuePos}</span>}
          </div>
        )}

        {(state === "done" || state === "failed" || state === "cancelled") && (
          <div className={`tc-d ${state}`}>
            <AgentAvatar agent={agent} size={18} />
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
              <b>{agent.name}</b>{" "}
              {task.reply ??
                (state === "failed"
                  ? "Run failed."
                  : state === "cancelled"
                    ? "Run cancelled."
                    : "Done.")}
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
