// Deuce ask-user extension for Pi.
//
// Pi has no native "agent is waiting on the human" event (verified in the U1
// spike). This extension gives the agent a blocking `ask_user` tool: when the
// agent calls it, ctx.ui.input emits an `extension_ui_request` on the RPC
// stdout stream and blocks until the client sends a matching
// `extension_ui_response`. The Deuce runtime maps that request to the task's
// `awaiting_input` state and routes the human's drawer reply back as the
// response (KTD15 / R7 / R16 / AE3).
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
      "context) instead of guessing. Returns the user's answer as text.",
    parameters: Type.Object({
      question: Type.String({
        description: "The question to ask the user, phrased clearly.",
      }),
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

      const answer = await ctx.ui.input("A question for you", params.question);
      return {
        content: [{ type: "text", text: answer ?? "" }],
        details: {},
      };
    },
  });
}
