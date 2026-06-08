// Deuce ask-user extension for Pi.
//
// Pi has no native "agent is waiting on the human" event (verified in the U1
// spike). This extension gives the agent a blocking `ask_user` tool: when the
// agent calls it, a ctx.ui primitive emits an `extension_ui_request` on the RPC
// stdout stream and blocks until the client sends a matching
// `extension_ui_response`. The Deuce runtime maps that request to the task's
// `awaiting_input` state and routes the human's drawer reply back as the
// response (KTD15 / R7 / R16 / AE3).
//
// The tool optionally carries a `kind` (free-text / pick-one / confirm) and,
// for choice kinds, an `options` list, so the client can render a typed prompt
// (text field / buttons / yes-no) instead of a bare text box. `kind`/`options`
// are additive: omitting them preserves the original free-text behavior. The
// richer ctx.ui primitives (select/confirm) are feature-detected at runtime —
// when the running Pi build does not expose them, the tool falls back to
// ctx.ui.input with the options enumerated in the prompt. Either way it returns
// the answer as plain text and never emits raw JSON to the user.
//
// Auto-discovered when placed at ~/.pi/agent/extensions/ in the container.

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

export default function (pi: ExtensionAPI) {
  pi.registerTool({
    name: "ask_user",
    label: "Ask the user",
    description:
      "Ask the human a clarifying question and block until they answer. " +
      "Use this whenever you are blocked on a decision only the user can make " +
      "(ambiguous requirements, a risky action needing approval, missing " +
      "context) instead of guessing. " +
      "Set kind to 'select' and provide options when the answer is one of a " +
      "small set of choices, or kind 'confirm' for a yes/no decision; omit " +
      "kind for an open-ended text answer. Returns the user's answer as text.",
    parameters: Type.Object({
      question: Type.String({
        description: "The question to ask the user, phrased clearly.",
      }),
      kind: Type.Optional(
        Type.Union([
          Type.Literal("input"),
          Type.Literal("select"),
          Type.Literal("confirm"),
        ], {
          description:
            "How the user answers: 'input' (free text, default), 'select' " +
            "(pick one of options), or 'confirm' (yes/no).",
        }),
      ),
      options: Type.Optional(
        Type.Array(Type.String(), {
          description:
            "Choices to offer when kind is 'select'. Ignored for other kinds.",
        }),
      ),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      // In headless contexts with no UI channel, don't block forever — tell the
      // agent to proceed on its best judgment rather than hang.
      if (!ctx.hasUI) {
        return {
          content: [
            {
              type: "text",
              text: "No interactive channel is available to ask the user; proceed using your best judgment.",
            },
          ],
          details: {},
        };
      }

      const ui = ctx.ui as Record<string, unknown>;
      const options = Array.isArray(params.options) ? params.options : [];
      // Infer select when options were supplied without an explicit kind.
      const kind =
        params.kind ?? (options.length > 0 ? "select" : "input");

      const text = (answer: unknown): string =>
        answer == null ? "" : String(answer);

      let answer: unknown;
      if (kind === "select" && options.length > 0) {
        if (typeof ui.select === "function") {
          answer = await (ui.select as (
            title: string,
            prompt: string,
            options: string[],
          ) => Promise<unknown>)("A question for you", params.question, options);
        } else {
          // Fallback: enumerate the options in a text prompt. The answer is
          // still plain text — never JSON.
          const list = options.map((o, i) => `${i + 1}. ${o}`).join("\n");
          answer = await ctx.ui.input(
            "A question for you",
            `${params.question}\n\nOptions:\n${list}`,
          );
        }
      } else if (kind === "confirm") {
        if (typeof ui.confirm === "function") {
          const ok = await (ui.confirm as (
            title: string,
            prompt: string,
          ) => Promise<unknown>)("A question for you", params.question);
          answer = ok ? "yes" : "no";
        } else {
          answer = await ctx.ui.input(
            "A question for you",
            `${params.question} (yes/no)`,
          );
        }
      } else {
        answer = await ctx.ui.input("A question for you", params.question);
      }

      return {
        content: [{ type: "text", text: text(answer) }],
        details: {},
      };
    },
  });
}
