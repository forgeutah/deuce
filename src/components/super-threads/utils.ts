// Super Threads helpers shared across the card/drawer components. Kept separate
// from the .tsx component modules so fast-refresh's "components only" rule holds.

import type { TaskState } from "@/types";

// stripMention drops the leading @agent token from a prompt for compact display.
export function stripMention(text: string): string {
  return text.replace(/@\w+/, "").replace(/^[\s,]+/, "").trim();
}

// taskFallbackMessage is the display text for a terminal task that produced no
// reply. Shared by the inline card and the thread drawer so the phrasing stays
// in one place.
export function taskFallbackMessage(state: TaskState): string {
  return state === "failed"
    ? "Run failed."
    : state === "cancelled"
      ? "Run cancelled."
      : "Done.";
}

// askUserQuestion returns the display question for an ask_user tool action, or
// null when the action isn't one. Two shapes exist (R9 — a question must never
// render as raw JSON):
//   - "Ask": post-fix rows where the server already extracted the question; the
//     arg passes through verbatim (even JSON-looking question text — never
//     re-parsed, the server settled it).
//   - "Ask_user": legacy persisted rows whose arg is the raw args object
//     (`{"question":"..."}`); parse it for the question, degrading to a
//     readable label when the JSON is truncated or the key is missing.
// arg can be undefined (a synthesized row from an action_completed seen
// without its action_started carries no arg) — degrade to the label.
export function askUserQuestion(
  tool: string,
  arg: string | undefined,
): string | null {
  if (tool === "Ask") {
    return arg && arg.trim() !== "" ? arg : "(question unavailable)";
  }
  if (tool !== "Ask_user") return null;
  try {
    const parsed: unknown = JSON.parse(arg ?? "");
    if (parsed && typeof parsed === "object") {
      const q = (parsed as { question?: unknown }).question;
      if (typeof q === "string" && q.trim() !== "") return q.trim();
    }
  } catch {
    // Truncated/garbled legacy arg — fall through to the readable label.
  }
  return "(question unavailable)";
}
