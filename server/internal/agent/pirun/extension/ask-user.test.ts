// Behavioral suite for the ask-user Pi extension (U1 step 8).
//
// Why this exists: the original bug was an argument-order error against Pi's
// `ExtensionUIContext` — `select(title, options, opts)` called as
// `select(title, question, options)`. Nothing caught it. A type-check catches
// it now (U1 step 6), but a type-check cannot assert that the *question text*
// lands in the field the drawer reads, that a timed-out yes/no is told apart
// from a real "No", or that every dialog carries the timeout the cross-language
// KTD7 invariant depends on. That is what this suite asserts.
//
// The expected wire shapes are not invented here: they are read from
// ../testdata/pi-ui-protocol.json, the contract fixture transcribed from Pi's
// published package. Both sides of the wire assert against that one file.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import type {
  ExtensionAPI,
  ExtensionContext,
  ExtensionUIContext,
  ExtensionUIDialogOptions,
} from "@earendil-works/pi-coding-agent";
import { afterEach, describe, expect, it, vi } from "vitest";

import askUser from "./ask-user";

// ---------------------------------------------------------------------------
// Contract fixture
// ---------------------------------------------------------------------------

interface FixtureRequest {
  name: string;
  method: string;
  blocking: boolean;
  line: Record<string, unknown>;
}

interface Fixture {
  piVersion: string;
  requests: FixtureRequest[];
  deuceAwaitCeilingMs: number;
}

const here = path.dirname(fileURLToPath(import.meta.url));
const fixture = JSON.parse(
  readFileSync(path.join(here, "..", "testdata", "pi-ui-protocol.json"), "utf8"),
) as Fixture;

function arm(name: string): Record<string, unknown> {
  const found = fixture.requests.find((r) => r.name === name);
  if (!found) throw new Error(`contract fixture has no request arm named ${name}`);
  return found.line;
}

// The ceiling Deuce's runtime applies to an unanswered question
// (defaultAwaitTimeout in server/internal/agent/runtime.go). Pi's dialog
// timeout must stay strictly above it (KTD7). Read from the fixture rather
// than hardcoded here: a hardcoded copy drifts silently when the Go constant
// moves. The Go side asserts defaultAwaitTimeout equals this same field, so
// the fixture is the one place the invariant's two halves meet.
const DEUCE_AWAIT_CEILING_MS = fixture.deuceAwaitCeilingMs;
if (typeof DEUCE_AWAIT_CEILING_MS !== "number") {
  throw new Error("contract fixture has no numeric deuceAwaitCeilingMs");
}

// ---------------------------------------------------------------------------
// Pi harness
// ---------------------------------------------------------------------------

/** A dialog the extension opened, with the JSONL line Pi would have emitted. */
interface DialogCall {
  method: string;
  args: unknown[];
  line: Record<string, unknown>;
}

/** Sentinel for "this dialog never resolves on its own". */
const PENDING = Symbol("pending");
type Scripted = string | boolean | undefined | typeof PENDING;

interface Script {
  select?: Scripted;
  confirm?: Scripted;
  input?: Scripted;
}

interface Harness {
  ui: ExtensionUIContext;
  calls: DialogCall[];
}

/**
 * A stand-in for Pi's RPC `ExtensionUIContext`.
 *
 * The line-building and abort semantics below are transcribed from
 * `dist/modes/rpc/rpc-mode.js` in @earendil-works/pi-coding-agent 0.84.0
 * (`createDialogPromise` + `createExtensionUIContext`): the per-method request
 * object is spread FLAT after `{type, id}` and JSON-serialized to stdout, which
 * drops undefined-valued keys; and an aborted or timed-out dialog *resolves*
 * with the method's default value rather than rejecting — `undefined` for
 * select/input, `false` for confirm. That last detail is the whole reason R13
 * exists.
 */
function makePi(script: Script): Harness {
  const calls: DialogCall[] = [];
  let seq = 0;

  function dialog(
    method: string,
    request: Record<string, unknown>,
    args: unknown[],
    opts: ExtensionUIDialogOptions | undefined,
    defaultValue: unknown,
  ): Promise<unknown> {
    const id = `ui-${++seq}`;
    const line = JSON.parse(
      JSON.stringify({ type: "extension_ui_request", id, ...request }),
    ) as Record<string, unknown>;
    calls.push({ method, args, line });

    return new Promise((resolve) => {
      if (opts?.signal?.aborted) {
        resolve(defaultValue);
        return;
      }
      opts?.signal?.addEventListener("abort", () => resolve(defaultValue), {
        once: true,
      });
      const scripted = method in script
        ? (script as Record<string, Scripted>)[method]
        : PENDING;
      if (scripted !== PENDING) resolve(scripted);
    });
  }

  const ui = {
    select: (title: string, options: string[], opts?: ExtensionUIDialogOptions) =>
      dialog(
        "select",
        { method: "select", title, options, timeout: opts?.timeout },
        [title, options, opts],
        opts,
        undefined,
      ),
    confirm: (title: string, message: string, opts?: ExtensionUIDialogOptions) =>
      dialog(
        "confirm",
        { method: "confirm", title, message, timeout: opts?.timeout },
        [title, message, opts],
        opts,
        false,
      ),
    input: (title: string, placeholder?: string, opts?: ExtensionUIDialogOptions) =>
      dialog(
        "input",
        { method: "input", title, placeholder, timeout: opts?.timeout },
        [title, placeholder, opts],
        opts,
        undefined,
      ),
  } as unknown as ExtensionUIContext;

  return { ui, calls };
}

// ---------------------------------------------------------------------------
// Extension harness
// ---------------------------------------------------------------------------

interface AskParams {
  question: string;
  kind?: "input" | "select" | "confirm";
  options?: string[];
}

type ToolResult = { content: { type: string; text: string }[] };

interface CapturedTool {
  name: string;
  execute: (
    toolCallId: string,
    params: AskParams,
    signal: AbortSignal | undefined,
    onUpdate: undefined,
    ctx: ExtensionContext,
  ) => Promise<ToolResult>;
}

function loadTool(): CapturedTool {
  let captured: CapturedTool | undefined;
  const pi = {
    registerTool(tool: unknown) {
      captured = tool as CapturedTool;
    },
  };
  askUser(pi as unknown as ExtensionAPI);
  if (!captured) throw new Error("extension registered no tool");
  return captured;
}

function makeCtx(ui: ExtensionUIContext, hasUI = true): ExtensionContext {
  return { hasUI, mode: "rpc", ui } as unknown as ExtensionContext;
}

/** Run the tool against a scripted Pi and return both sides of the exchange. */
function ask(
  params: AskParams,
  script: Script,
  signal?: AbortSignal,
): { result: Promise<ToolResult>; calls: DialogCall[] } {
  const pi = makePi(script);
  const result = loadTool().execute("call-1", params, signal, undefined, makeCtx(pi.ui));
  return { result, calls: pi.calls };
}

function textOf(result: ToolResult): string {
  return result.content.map((c) => c.text).join("");
}

afterEach(() => {
  vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// Request shape — one test per arm of Pi's published union
// ---------------------------------------------------------------------------

describe("ask_user request shape", () => {
  it("emits a pick-one dialog matching the fixture's select arm", async () => {
    const expected = arm("select");
    const question = expected.title as string;
    const options = expected.options as string[];

    const { result, calls } = ask(
      { question, kind: "select", options },
      { select: options[1] },
    );
    await result;

    expect(calls).toHaveLength(1);
    // Whole-line equality against the contract fixture, id aside (Pi mints a
    // uuid). This is the assertion the original argument-order bug fails:
    // `options` would be the question string, not the array.
    expect({ ...calls[0].line, id: expected.id }).toEqual(expected);
    expect(Array.isArray(calls[0].line.options)).toBe(true);
    expect(calls[0].line.title).toBe(question);
  });

  it("infers pick-one when options are supplied without an explicit kind", async () => {
    const { result, calls } = ask(
      { question: "Which framework?", options: ["React", "Vue"] },
      { select: "Vue" },
    );
    await result;

    expect(calls[0].method).toBe("select");
    expect(calls[0].line.options).toEqual(["React", "Vue"]);
  });

  it("emits a yes/no dialog matching the fixture's empty-message confirm arm", async () => {
    const expected = arm("confirm_empty_message");
    const question = expected.title as string;

    const { result, calls } = ask({ question, kind: "confirm" }, { confirm: true });
    await result;

    expect({ ...calls[0].line, id: expected.id }).toEqual(expected);
  });

  it("carries a yes/no question in the title, not behind a boilerplate prefix", async () => {
    const { result, calls } = ask(
      { question: "Delete the stale branches?", kind: "confirm" },
      { confirm: false },
    );
    await result;

    // The old code titled every confirm "A question for you" and put the
    // question in `message`, so the drawer rendered the boilerplate first.
    expect(calls[0].line.title).toBe("Delete the stale branches?");
    expect(calls[0].line.message).toBe("");
  });

  it("carries a free-text question in the title, never only as a placeholder", async () => {
    const question = "Which environment should I deploy to?";
    const { result, calls } = ask({ question }, { input: "staging" });
    await result;

    const contract = arm("input");
    // Structural conformance: no key outside Pi's published input arm.
    expect(Object.keys(calls[0].line).every((k) => k in contract)).toBe(true);
    expect(calls[0].line.method).toBe("input");
    expect(calls[0].line.title).toBe(question);
    expect(calls[0].line.timeout).toBe(contract.timeout);
    // Pi's second argument is placeholder text, not the prompt body. The
    // question must not be smuggled through it.
    expect(calls[0].line.placeholder).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// KTD7 — the cross-language timeout invariant
// ---------------------------------------------------------------------------

describe("dialog options", () => {
  const cases: { style: string; params: AskParams; script: Script }[] = [
    {
      style: "select",
      params: { question: "Which?", kind: "select", options: ["a", "b"] },
      script: { select: "a" },
    },
    { style: "confirm", params: { question: "Sure?", kind: "confirm" }, script: { confirm: true } },
    { style: "input", params: { question: "Where?" }, script: { input: "here" } },
  ];

  for (const { style, params, script } of cases) {
    it(`passes ${style} a timeout above Deuce's ceiling and an abort signal`, async () => {
      const { result, calls } = ask(params, script);
      await result;

      const opts = calls[0].args[2] as ExtensionUIDialogOptions;
      expect(opts.timeout).toBeGreaterThan(DEUCE_AWAIT_CEILING_MS);
      // The fixture records the exact value the Go side expects to see.
      expect(opts.timeout).toBe(arm("select").timeout);
      expect(opts.signal).toBeInstanceOf(AbortSignal);
      expect(opts.signal?.aborted).toBe(false);
    });
  }
});

// ---------------------------------------------------------------------------
// R13 / AE7 — no-answer must never look like an answer
// ---------------------------------------------------------------------------

describe("no answer received", () => {
  it("returns the explicit no-answer text when a free-text dialog times out", async () => {
    vi.useFakeTimers();
    // Nothing scripted: the dialog stays open until the extension's own
    // deadline dismisses it.
    const { result } = ask({ question: "Where?" }, {});

    await vi.advanceTimersByTimeAsync(35 * 60 * 1000);
    const text = textOf(await result);

    expect(text).toContain("No answer was received");
    expect(text).not.toBe("");
  });

  it("distinguishes a timed-out yes/no from a real No", async () => {
    // A real No.
    const answered = textOf(await ask({ question: "Sure?", kind: "confirm" }, { confirm: false }).result);
    expect(answered).toBe("no");

    // A timeout. Pi resolves this to `false` too — the same value the real No
    // produced — so only the extension's own flag can tell them apart.
    vi.useFakeTimers();
    const { result } = ask({ question: "Sure?", kind: "confirm" }, {});
    await vi.advanceTimersByTimeAsync(35 * 60 * 1000);
    const timedOut = textOf(await result);

    expect(timedOut).not.toBe("no");
    expect(timedOut).toContain("Do not assume yes or no");
  });

  it("returns the no-answer text when the tool's own abort signal fires", async () => {
    const controller = new AbortController();
    const { result, calls } = ask(
      { question: "Which?", kind: "select", options: ["a", "b"] },
      {},
      controller.signal,
    );

    controller.abort();
    const text = textOf(await result);

    expect(text).toContain("No answer was received");
    // The dialog was opened and then dismissed, not skipped.
    expect(calls).toHaveLength(1);
  });

  it("does not report a no-answer when the user actually answers", async () => {
    const text = textOf(
      await ask({ question: "Which?", kind: "select", options: ["a", "b"] }, { select: "b" }).result,
    );
    expect(text).toBe("b");
  });
});

// ---------------------------------------------------------------------------
// Headless guard
// ---------------------------------------------------------------------------

describe("no UI channel", () => {
  it("returns the proceed-on-best-judgment result without opening a dialog", async () => {
    const pi = makePi({});
    const result = await loadTool().execute(
      "call-1",
      { question: "Which?" },
      undefined,
      undefined,
      makeCtx(pi.ui, false),
    );

    expect(textOf(result)).toContain("proceed using your best judgment");
    expect(pi.calls).toHaveLength(0);
  });
});
