// askUserQuestion — display extraction for ask_user tool actions (R9: a
// question never renders as raw JSON in the action log or task card).

import { describe, expect, it } from "vitest";

import { askUserQuestion } from "./utils";

describe("askUserQuestion", () => {
  it("passes a post-fix Ask row's arg through verbatim", () => {
    expect(askUserQuestion("Ask", "Which file?")).toBe("Which file?");
  });

  it("does not re-parse an Ask row whose question text is itself JSON", () => {
    // The server already extracted the question; JSON-looking text is the
    // question, not a payload to unwrap.
    expect(askUserQuestion("Ask", '{"not":"a question"}')).toBe(
      '{"not":"a question"}',
    );
  });

  it("extracts the question from a legacy Ask_user JSON arg", () => {
    expect(askUserQuestion("Ask_user", '{"question":"Which file?"}')).toBe(
      "Which file?",
    );
  });

  it("extracts just the question from a legacy select-kind arg", () => {
    const arg =
      '{"question":"Which framework?","kind":"select","options":["React","Vue"]}';
    expect(askUserQuestion("Ask_user", arg)).toBe("Which framework?");
  });

  it("unescapes escaped content in a legacy arg", () => {
    expect(
      askUserQuestion("Ask_user", '{"question":"Name it \\"deuce\\"?\\nSure?"}'),
    ).toBe('Name it "deuce"?\nSure?');
  });

  it("degrades a truncated legacy arg to a readable label, never throws", () => {
    expect(askUserQuestion("Ask_user", '{"question":"Which fi')).toBe(
      "(question unavailable)",
    );
  });

  it("degrades a legacy arg with no question key to a readable label", () => {
    expect(askUserQuestion("Ask_user", '{"kind":"confirm"}')).toBe(
      "(question unavailable)",
    );
  });

  it("degrades a legacy arg with a non-string question to a readable label", () => {
    expect(askUserQuestion("Ask_user", '{"question":{"nested":true}}')).toBe(
      "(question unavailable)",
    );
  });

  it("degrades an Ask row with a missing or empty arg to the readable label", () => {
    // A synthesized row (action_completed seen without its action_started)
    // carries no arg.
    expect(askUserQuestion("Ask", undefined)).toBe("(question unavailable)");
    expect(askUserQuestion("Ask", "")).toBe("(question unavailable)");
    expect(askUserQuestion("Ask", "   ")).toBe("(question unavailable)");
  });

  it("degrades an Ask_user row with a missing arg, never throws", () => {
    expect(askUserQuestion("Ask_user", undefined)).toBe(
      "(question unavailable)",
    );
  });

  it("returns null for non-question tools, even with JSON-looking args", () => {
    expect(askUserQuestion("Bash", '{"question":"trick"}')).toBeNull();
    expect(askUserQuestion("Custom_tool", '{"foo":1}')).toBeNull();
    expect(askUserQuestion("Think", "")).toBeNull();
  });
});
