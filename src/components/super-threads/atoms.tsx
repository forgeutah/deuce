// Super Threads shared atoms — small presentational pieces shared by the
// inline task card and the per-agent thread drawer. Ported from the design
// prototype (queue-app.jsx): AgentAvatar (AvA), TypingDots (Tdots), Mentioned,
// and the Claude Code-style ActionLog.

import {
  FileText,
  Search,
  Pencil,
  FilePlus,
  SquareTerminal,
  Sparkles,
  Check,
  Loader2,
  CircleDot,
  AlertCircle,
  type LucideIcon,
} from "lucide-react";
import type { Agent, AgentAction } from "@/types";

// Tool → icon, mirroring TOOL_ICON in the prototype. Unknown tools fall back to
// a neutral dot.
const TOOL_ICON: Record<string, LucideIcon> = {
  Read: FileText,
  Grep: Search,
  Edit: Pencil,
  Write: FilePlus,
  Bash: SquareTerminal,
  Think: Sparkles,
};

// AgentAvatar is the colored initial square used wherever an agent appears.
export function AgentAvatar({ agent, size = 22 }: { agent: Agent; size?: number }) {
  return (
    <div
      className="av"
      style={{
        background: agent.color,
        width: size,
        height: size,
        fontSize: size * 0.42,
        borderRadius: 5,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        color: "#06121f",
        fontWeight: 700,
        flexShrink: 0,
      }}
    >
      {agent.name[0]}
    </div>
  );
}

// TypingDots renders the three pulsing dots in the agent's color.
export function TypingDots({ color }: { color: string }) {
  return (
    <div style={{ display: "flex", gap: 4 }}>
      {[0, 0.2, 0.4].map((d, i) => (
        <span
          key={i}
          className="tdot"
          style={{ background: color, animationDelay: `${d}s` }}
        />
      ))}
    </div>
  );
}

// Mentioned highlights @mentions inline in the agent's color.
export function Mentioned({ text, color }: { text: string; color?: string }) {
  return (
    <>
      {text.split(/(@\w+)/g).map((part, i) =>
        /^@\w+$/.test(part) ? (
          <span
            key={i}
            className="mention"
            style={{ color: color ?? "var(--color-accent)", fontWeight: 600 }}
          >
            {part}
          </span>
        ) : (
          <span key={i}>{part}</span>
        ),
      )}
    </>
  );
}

function statusClass(action: AgentAction): "run" | "done" | "error" {
  if (action.status === "started") return "run";
  if (action.status === "error" || action.isError) return "error";
  return "done";
}

function ActionItem({ action }: { action: AgentAction }) {
  const cls = statusClass(action);
  const StatIcon = cls === "run" ? Loader2 : cls === "error" ? AlertCircle : Check;

  if (action.tool === "Think") {
    return (
      <div className="q-act think">
        <div className="q-act-row">
          <span className="q-act-ic">
            <Sparkles size={13} />
          </span>
          <span>
            <span className="tool" style={{ fontStyle: "normal" }}>
              Thinking
            </span>
            {action.text ? ` — ${action.text}` : ""}
          </span>
          {cls === "run" && (
            <span className="q-act-stat run">
              <Loader2 size={13} />
            </span>
          )}
        </div>
      </div>
    );
  }

  const ToolIcon = TOOL_ICON[action.tool] ?? CircleDot;
  return (
    <div className="q-act">
      <div className="q-act-row">
        <span className="q-act-ic">
          <ToolIcon size={13} />
        </span>
        <span>
          <span className="tool">{action.tool}</span>
          <span className="paren">(</span>
          <span className="arg">{action.arg}</span>
          <span className="paren">)</span>
          {action.stat && <span className="note">{action.stat}</span>}
        </span>
        <span className={`q-act-stat ${cls}`}>
          <StatIcon size={13} />
        </span>
      </div>
      {/* Completed tool output (Bash stdout, etc.). Diffs from Edit/Write come
          through as `text` too — rendered verbatim in a mono block. */}
      {cls !== "run" && action.text && <pre className="q-out">{action.text}</pre>}
    </div>
  );
}

// ActionLog renders an agent task's tool stream in order. Started actions show a
// spinner; completed/errored ones show their result.
export function ActionLog({ actions }: { actions: AgentAction[] }) {
  if (!actions.length) return null;
  return (
    <div className="q-actions">
      {actions.map((a) => (
        <ActionItem key={a.callId} action={a} />
      ))}
    </div>
  );
}
