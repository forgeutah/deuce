import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, act, screen } from "@testing-library/react";

// Declared through vi.hoisted so the vi.mock factories below — which are
// hoisted to the top of the module — can reach them.
const { FakeTerminal, FakeWebSocket } = vi.hoisted(() => {
  // A stand-in for xterm.js. `write` queues its payload and completion
  // callback instead of running it synchronously, mirroring the real
  // terminal's async parse — the behaviour the replay gate depends on. Tests
  // drive the parse explicitly with `flush()`.
  class FakeTerminal {
    static last: FakeTerminal | undefined;

    cols = 80;
    rows = 24;
    written: string[] = [];
    disposed = false;
    focusCount = 0;

    private queue: Array<{ data: string; cb?: () => void }> = [];
    private dataHandler: ((d: string) => void) | undefined;

    constructor() {
      FakeTerminal.last = this;
    }

    loadAddon() {}
    open() {}
    focus() {
      this.focusCount++;
    }
    dispose() {
      this.disposed = true;
    }

    onData(handler: (d: string) => void) {
      this.dataHandler = handler;
    }
    onResize() {}

    write(data: string | Uint8Array, cb?: () => void) {
      const text =
        typeof data === "string" ? data : new TextDecoder().decode(data);
      this.queue.push({ data: text, cb });
    }

    /** Parse everything queued, running completion callbacks in order. */
    flush() {
      const pending = this.queue;
      this.queue = [];
      for (const { data, cb } of pending) {
        if (data) this.written.push(data);
        cb?.();
      }
    }

    /** Simulate the terminal emitting bytes back — a keystroke or a reply. */
    emitData(d: string) {
      this.dataHandler?.(d);
    }
  }

  class FakeWebSocket {
    static OPEN = 1;
    static last: FakeWebSocket | undefined;

    readyState = 1;
    binaryType = "";
    sent: Uint8Array[] = [];
    onopen: (() => void) | null = null;
    onmessage: ((e: { data: ArrayBuffer }) => void) | null = null;
    onclose: (() => void) | null = null;
    onerror: (() => void) | null = null;

    constructor() {
      FakeWebSocket.last = this;
    }

    send(payload: Uint8Array) {
      this.sent.push(payload);
    }
    close() {}
  }

  return { FakeTerminal, FakeWebSocket };
});

type FakeTerminal = InstanceType<typeof FakeTerminal>;
type FakeWebSocket = InstanceType<typeof FakeWebSocket>;

vi.mock("@xterm/xterm", () => ({ Terminal: FakeTerminal }));
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit() {} } }));
vi.mock("@xterm/addon-web-links", () => ({ WebLinksAddon: class {} }));
vi.mock("@xterm/xterm/css/xterm.css", () => ({}));
vi.mock("@/stores/session-store", () => ({
  useSessionStore: () => ({
    activeSessionId: "session-1",
    sessions: [{ id: "session-1", workspaceStatus: "ready" }],
  }),
}));

import { TerminalView } from "./TerminalView";

/** Build a server→client frame: one prefix byte plus an optional payload. */
function frame(prefix: number, body = ""): ArrayBuffer {
  const encoded = new TextEncoder().encode(body);
  const buf = new Uint8Array(1 + encoded.length);
  buf[0] = prefix;
  buf.set(encoded, 1);
  return buf.buffer;
}

/** Decode the outbound frames of a given prefix into strings. */
function outbound(ws: FakeWebSocket, prefix = 0x00): string[] {
  return ws.sent
    .filter((p) => p[0] === prefix)
    .map((p) => new TextDecoder().decode(p.subarray(1)));
}

function setup() {
  render(<TerminalView />);
  const term = FakeTerminal.last!;
  const ws = FakeWebSocket.last!;
  act(() => {
    ws.onopen?.();
  });
  return { term, ws };
}

function deliver(ws: FakeWebSocket, prefix: number, body = "") {
  act(() => {
    ws.onmessage?.({ data: frame(prefix, body) });
  });
}

// The exact reply xterm produces for an OSC 11 query against this app's theme.
const OSC11_QUERY = "\x1b]11;?\x07";
const OSC11_REPLY = "\x1b]11;rgb:0d0d/1111/1717\x1b\\";
const DSR_QUERY = "\x1b[6n";
const CPR_REPLY = "\x1b[24;1R";

describe("TerminalView replay gate", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", FakeWebSocket);
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
      },
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
    FakeTerminal.last = undefined;
    FakeWebSocket.last = undefined;
  });

  it("sends nothing to the PTY before the replay boundary arrives", () => {
    const { term, ws } = setup();

    act(() => term.emitData("ls -la\r"));

    expect(outbound(ws)).toEqual([]);
  });

  it("resumes sending keystrokes once replay is complete and parsed", () => {
    const { term, ws } = setup();

    deliver(ws, 0x03);
    act(() => term.flush());
    act(() => term.emitData("ls -la\r"));

    expect(outbound(ws)).toEqual(["ls -la\r"]);
  });

  it("renders replayed output so the terminal is not blank on reconnect", () => {
    const { term, ws } = setup();

    deliver(ws, 0x02, "previous scrollback");
    act(() => term.flush());

    expect(term.written).toContain("previous scrollback");
  });

  it("renders live output", () => {
    const { term, ws } = setup();

    deliver(ws, 0x03);
    act(() => term.flush());
    deliver(ws, 0x00, "live shell output");
    act(() => term.flush());

    expect(term.written).toContain("live shell output");
  });

  it("stays muted until the replayed bytes have actually been parsed", () => {
    // The regression guard for the async-write trap: receiving 0x03 is not
    // sufficient, because xterm may not have reached the queued 0x02 content
    // yet — and its query replies fire during that parse.
    const { term, ws } = setup();

    deliver(ws, 0x02, OSC11_QUERY);
    deliver(ws, 0x03);

    // 0x03 has landed but nothing has been parsed. A reply now must not escape.
    act(() => term.emitData(OSC11_REPLY));
    expect(outbound(ws)).toEqual([]);

    act(() => term.flush());
    act(() => term.emitData("x"));
    expect(outbound(ws)).toEqual(["x"]);
  });

  it("does not echo an OSC 11 colour reply triggered by replayed output", () => {
    // The reported bug: switching to the Terminal tab pasted
    // `11;rgb:0d0d/1111/1717` at the shell prompt.
    const { term, ws } = setup();

    deliver(ws, 0x02, `some scrollback${OSC11_QUERY}more scrollback`);
    act(() => term.emitData(OSC11_REPLY));
    deliver(ws, 0x03);
    act(() => term.flush());

    expect(outbound(ws).join("")).not.toContain("rgb:");
    expect(ws.sent).toHaveLength(1); // the initial resize frame only
  });

  it("does not echo a cursor-position reply triggered by replayed output", () => {
    // Proves the gate is class-wide rather than OSC-specific.
    const { term, ws } = setup();

    deliver(ws, 0x02, `scrollback${DSR_QUERY}`);
    act(() => term.emitData(CPR_REPLY));
    deliver(ws, 0x03);
    act(() => term.flush());

    expect(outbound(ws).join("")).not.toContain("R");
  });

  it("answers queries normally once replay has completed", () => {
    // A TUI started after connect still needs its colour/capability answers.
    const { term, ws } = setup();

    deliver(ws, 0x03);
    act(() => term.flush());
    deliver(ws, 0x00, OSC11_QUERY);
    act(() => term.flush());
    act(() => term.emitData(OSC11_REPLY));

    expect(outbound(ws)).toEqual([OSC11_REPLY]);
  });

  it("unmutes on the fallback timer when no boundary frame ever arrives", () => {
    // An older server never sends 0x03; a permanently muted terminal would be
    // a worse failure than the bug this fixes.
    vi.useFakeTimers();
    const { term, ws } = setup();

    act(() => term.emitData("early"));
    expect(outbound(ws)).toEqual([]);

    act(() => {
      vi.advanceTimersByTime(2000);
    });
    act(() => term.emitData("late"));

    expect(outbound(ws)).toEqual(["late"]);
  });

  it("disarms the fallback once a replay frame proves the server is current", () => {
    // A slow link delivering a large replay must not trip the timer and open
    // the gate mid-replay — the very thing the gate exists to prevent.
    vi.useFakeTimers();
    const { term, ws } = setup();

    deliver(ws, 0x02, "a lot of scrollback");
    act(() => {
      vi.advanceTimersByTime(10_000);
    });
    act(() => term.emitData(OSC11_REPLY));
    expect(outbound(ws)).toEqual([]);

    // …and the boundary still unmutes normally when it eventually lands.
    deliver(ws, 0x03);
    act(() => term.flush());
    act(() => term.emitData("x"));
    expect(outbound(ws)).toEqual(["x"]);
  });

  it("still sends resize frames while muted", () => {
    // The gate is scoped to onData; resize is not a query reply.
    const { ws } = setup();

    const resizes = ws.sent.filter((p) => p[0] === 0x01);
    expect(resizes).toHaveLength(1);
    expect(JSON.parse(new TextDecoder().decode(resizes[0].subarray(1)))).toEqual({
      cols: 80,
      rows: 24,
    });
  });

  it("focuses the terminal on mount so selecting the tab lands the cursor", () => {
    const { term } = setup();

    expect(term.focusCount).toBe(1);
  });

  it("shows a loading indicator while the terminal is still blank", () => {
    vi.useFakeTimers();
    setup();

    // Nothing yet — the indicator is delayed to avoid flickering on a fast
    // attach, so it must not be up immediately.
    expect(screen.queryByRole("status")).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(250);
    });

    expect(screen.getByRole("status")).toHaveTextContent("Starting terminal");
  });

  it("does not flash the indicator when output arrives promptly", () => {
    vi.useFakeTimers();
    const { ws } = setup();

    deliver(ws, 0x02, "previous scrollback");
    act(() => {
      vi.advanceTimersByTime(5000);
    });

    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("hides the loading indicator once the first output arrives", () => {
    vi.useFakeTimers();
    const { ws } = setup();

    act(() => {
      vi.advanceTimersByTime(250);
    });
    expect(screen.getByRole("status")).toBeInTheDocument();

    deliver(ws, 0x00, "user@container:~$ ");

    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("keeps the indicator up when only the replay boundary has arrived", () => {
    // `devpod ssh` spawns fast but connects slowly, so 0x03 lands on a blank
    // screen. Treating it as ready would drop the spinner too early.
    vi.useFakeTimers();
    const { term, ws } = setup();

    deliver(ws, 0x03);
    act(() => term.flush());
    act(() => {
      vi.advanceTimersByTime(250);
    });

    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("clears the fallback timer on unmount", () => {
    vi.useFakeTimers();
    const { unmount } = render(<TerminalView />);
    const ws = FakeWebSocket.last!;
    act(() => {
      ws.onopen?.();
    });

    unmount();
    // An un-cleared timer would fire against a disposed terminal.
    expect(vi.getTimerCount()).toBe(0);
  });
});
