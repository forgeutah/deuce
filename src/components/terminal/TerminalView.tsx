import { useEffect, useRef } from "react";
import { Loader2, AlertCircle } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { useSessionStore } from "@/stores/session-store";

export function TerminalView() {
  const { activeSessionId, sessions } = useSessionStore();
  const terminalRef = useRef<HTMLDivElement>(null);

  const session = sessions.find((s) => s.id === activeSessionId);

  useEffect(() => {
    if (!terminalRef.current || !activeSessionId || session?.workspaceStatus !== "ready") return;

    let disposed = false;
    let ws: WebSocket | null = null;

    // The server replays recent PTY output to every newly-attached client so
    // the terminal isn't blank on reconnect. That buffer can contain terminal
    // *queries* the remote shell emitted earlier (OSC 11 background-colour,
    // DA, DSR). xterm can't tell a replayed query from a live one and answers
    // it — and the answer would go straight to PTY stdin, where the shell
    // echoes it as garbage at the prompt.
    //
    // So: drop everything xterm produces until the server's replay-complete
    // frame (0x03) arrives AND xterm has finished parsing the replayed bytes.
    // Both halves matter — term.write() is async, so unmuting the moment the
    // frame lands would still let replay-triggered replies escape.
    let replayDone = false;
    let unmuteTimer: ReturnType<typeof setTimeout> | undefined;

    // Fallback for an older server that never sends 0x03 at all. Unmuting
    // late is a cosmetic regression; staying muted forever is an unusable
    // terminal. It is disarmed as soon as any replay frame proves the server
    // speaks this protocol — otherwise a slow link delivering a large replay
    // could trip the timer and open the gate mid-replay, which is the exact
    // failure the gate exists to prevent.
    const REPLAY_FALLBACK_MS = 2000;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      scrollback: 5000,
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Menlo, monospace",
      drawBoldTextInBrightColors: true,
      theme: {
        background: "#0d1117",
        foreground: "#e6edf3",
        cursor: "#e6edf3",
        selectionBackground: "#264f78",
        black: "#484f58",
        red: "#ff7b72",
        green: "#3fb950",
        yellow: "#d29922",
        blue: "#58a6ff",
        magenta: "#bc8cff",
        cyan: "#39d2c0",
        white: "#e6edf3",
      },
    });

    const fitAddon = new FitAddon();
    const webLinksAddon = new WebLinksAddon();

    term.loadAddon(fitAddon);
    term.loadAddon(webLinksAddon);
    term.open(terminalRef.current);
    fitAddon.fit();

    // Send keystrokes to WebSocket with 0x00 prefix
    term.onData((data) => {
      if (!replayDone) return;
      if (ws && ws.readyState === WebSocket.OPEN) {
        const encoded = new TextEncoder().encode(data);
        const payload = new Uint8Array(1 + encoded.length);
        payload[0] = 0x00;
        payload.set(encoded, 1);
        ws.send(payload);
      }
    });

    // Send resize events
    term.onResize(({ cols, rows }) => {
      if (ws && ws.readyState === WebSocket.OPEN) {
        const resize = JSON.stringify({ cols, rows });
        const encoded = new TextEncoder().encode(resize);
        const payload = new Uint8Array(1 + encoded.length);
        payload[0] = 0x01;
        payload.set(encoded, 1);
        ws.send(payload);
      }
    });

    // Auto-fit on container resize
    const resizeObserver = new ResizeObserver(() => {
      if (!disposed) fitAddon.fit();
    });
    resizeObserver.observe(terminalRef.current);

    // Connect WebSocket
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws/terminal/${activeSessionId}`;
    ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      if (disposed) { ws?.close(); return; }
      unmuteTimer = setTimeout(() => {
        if (!disposed) replayDone = true;
      }, REPLAY_FALLBACK_MS);
      // Send initial terminal size
      const resize = JSON.stringify({ cols: term.cols, rows: term.rows });
      const encoded = new TextEncoder().encode(resize);
      const payload = new Uint8Array(1 + encoded.length);
      payload[0] = 0x01;
      payload.set(encoded, 1);
      ws!.send(payload);
    };

    ws.onmessage = (event) => {
      if (disposed) return;
      const data = new Uint8Array(event.data as ArrayBuffer);
      if (data.length < 1) return;

      switch (data[0]) {
        case 0x02:
          // Replayed output renders exactly like live output — the frame type
          // only decides whether we're allowed to answer it. Its arrival also
          // proves the server speaks this protocol, so the fallback can go.
          clearTimeout(unmuteTimer);
          if (data.length > 1) term.write(data.subarray(1));
          break;
        case 0x00: // live output
          if (data.length > 1) term.write(data.subarray(1));
          break;
        case 0x03: {
          // Replay boundary. Queue a zero-length write so the callback lands
          // behind every 0x02 chunk already queued — xterm processes writes
          // in order, so this fires only once the replay has been parsed and
          // any queries in it have already been answered into the void.
          clearTimeout(unmuteTimer);
          term.write("", () => {
            if (!disposed) replayDone = true;
          });
          break;
        }
      }
    };

    return () => {
      disposed = true;
      clearTimeout(unmuteTimer);
      resizeObserver.disconnect();
      if (ws) {
        ws.onmessage = null;
        ws.onclose = null;
        ws.onerror = null;
        // Closing during CONNECTING produces a noisy browser warning and
        // breaks under React StrictMode's double-mount. Leave onopen
        // attached so it sees `disposed` and closes cleanly once open.
        if (ws.readyState === WebSocket.OPEN) {
          ws.close();
        }
      }
      term.dispose();
    };
  }, [activeSessionId, session?.workspaceStatus]);

  if (!session) return null;

  if (session.workspaceStatus === "starting") {
    return (
      <div className="flex h-full items-center justify-center bg-background-inset">
        <div className="text-center">
          <Loader2 className="mx-auto h-8 w-8 text-warning animate-spin mb-3" />
          <p className="text-sm text-foreground-muted">
            Connecting to workspace...
          </p>
        </div>
      </div>
    );
  }

  if (session.workspaceStatus === "failed") {
    return (
      <div className="flex h-full items-center justify-center bg-background-inset">
        <div className="text-center">
          <AlertCircle className="mx-auto h-8 w-8 text-danger mb-3" />
          <p className="text-sm text-foreground-muted">Failed to connect</p>
          <button className="mt-2 text-xs text-accent hover:underline">
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div
      ref={terminalRef}
      className="h-full w-full bg-[#0d1117] p-3"
    />
  );
}
