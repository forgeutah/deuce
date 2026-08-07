// Deuce ask-user extension for Pi.
//
// Pi has no native "agent is waiting on the human" event (verified in the U1
// spike). This extension gives the agent a blocking `ask_user` tool: when the
// agent calls it, a ctx.ui dialog primitive emits an `extension_ui_request` on
// the RPC stdout stream and blocks until the client sends a matching
// `extension_ui_response`. The Deuce runtime maps that request to the task's
// `awaiting_input` state and routes the human's drawer reply back as the
// response (KTD15 / R7 / R16 / AE3).
//
// The tool optionally carries a `kind` (free-text / pick-one / confirm) and,
// for choice kinds, an `options` list, so the client can render a typed prompt
// (text field / buttons / yes-no) instead of a bare text box. `kind`/`options`
// are additive: omitting them preserves the original free-text behavior.
//
// PROTOCOL NOTE (the bug this file used to carry). Pi's `ExtensionUIContext`
// signatures — `dist/core/extensions/types.d.ts` in
// @earendil-works/pi-coding-agent — are:
//
//   select(title, options: string[], opts?)  -> Promise<string | undefined>
//   confirm(title, message: string, opts?)   -> Promise<boolean>
//   input(title, placeholder?, opts?)        -> Promise<string | undefined>
//
// There is no `(title, prompt)` form. Every style therefore carries the
// question in `title`; there is no second prose slot to put it in. The
// previous code passed the question as `select`'s second argument, so Pi
// emitted `options` as a *string* and Deuce's decoder dropped the whole line —
// no question ever reached the drawer. The shapes each call emits are pinned
// in ../testdata/pi-ui-protocol.json and asserted by ask-user.test.ts.
//
// Auto-discovered when placed at ~/.pi/agent/extensions/ in the container.

import type { ExtensionAPI, ExtensionUIDialogOptions } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

// Ceiling Pi applies to a dialog we never answer (R10). This is a backstop
// behind Deuce's own unanswered-question ceiling, never ahead of it: it MUST
// stay strictly greater than `defaultAwaitTimeout` in
// server/internal/agent/runtime.go (30 minutes), so Deuce's ceiling always
// fires first (KTD7). If Pi's timer won the race, Pi would resolve the dialog
// with its own default — `false` for confirm, `undefined` for select/input —
// and the model would receive a fabricated answer while the drawer still
// showed the question as answerable.
const PI_DIALOG_TIMEOUT_MS = 35 * 60 * 1000;

// Our own no-answer deadline, deliberately just under Pi's. Pi resolves a
// timed-out confirm to `false`, which is exactly what a real "No" resolves to
// — the resolved value alone cannot tell them apart (R13). So we abort the
// dialog ourselves a beat early and let a flag, not the resolved value, decide
// what the agent is told.
const NO_ANSWER_DEADLINE_MS = PI_DIALOG_TIMEOUT_MS - 30 * 1000;

const NO_ANSWER_TEXT =
  "No answer was received from the user — the question timed out or was " +
  "cancelled. Do not assume yes or no, and do not treat this as a decision. " +
  "Either proceed on your best judgment and say plainly that you did, or stop " +
  "and explain what you still need.";

const NO_UI_TEXT =
  "No interactive channel is available to ask the user; proceed using your best judgment.";

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
    async execute(_toolCallId, params, signal, _onUpdate, ctx) {
      // In headless contexts with no UI channel, don't block forever — tell the
      // agent to proceed on its best judgment rather than hang.
      if (!ctx.hasUI) {
        return {
          content: [{ type: "text", text: NO_UI_TEXT }],
          details: {},
        };
      }

      const options = Array.isArray(params.options) ? params.options : [];
      // Infer select when options were supplied without an explicit kind.
      const kind = params.kind ?? (options.length > 0 ? "select" : "input");

      // The no-answer flag (R13). Set when our deadline fires or when the
      // tool's own abort signal trips; either way the dialog is dismissed
      // through `controller` and Pi resolves it with a default we must not
      // hand to the model.
      const controller = new AbortController();
      let noAnswer = false;
      const giveUp = () => {
        if (noAnswer) return;
        noAnswer = true;
        controller.abort();
      };

      const deadline = setTimeout(giveUp, NO_ANSWER_DEADLINE_MS);
      signal?.addEventListener("abort", giveUp, { once: true });
      if (signal?.aborted) giveUp();

      const opts: ExtensionUIDialogOptions = {
        signal: controller.signal,
        timeout: PI_DIALOG_TIMEOUT_MS,
      };

      let answer: string;
      try {
        if (kind === "select" && options.length > 0) {
          // select(title, options, opts) — the question is the title; the
          // options array is the SECOND argument.
          const chosen = await ctx.ui.select(params.question, options, opts);
          answer = chosen ?? "";
        } else if (kind === "confirm") {
          // confirm(title, message, opts) — the question rides in the title so
          // yes/no prompts read the same as the other two styles; `message` is
          // the empty string because Pi's arm requires the field.
          const ok = await ctx.ui.confirm(params.question, "", opts);
          answer = ok ? "yes" : "no";
        } else {
          // input(title, placeholder?, opts) — the second argument is
          // placeholder text, not the prompt body, so the question goes in the
          // title and no placeholder is sent.
          const typed = await ctx.ui.input(params.question, undefined, opts);
          answer = typed ?? "";
        }
      } finally {
        clearTimeout(deadline);
        signal?.removeEventListener("abort", giveUp);
      }

      if (noAnswer) {
        return {
          content: [{ type: "text", text: NO_ANSWER_TEXT }],
          details: {},
        };
      }

      return {
        content: [{ type: "text", text: answer }],
        details: {},
      };
    },
  });
}
