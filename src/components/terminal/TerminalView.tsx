import { useEffect, useRef, useCallback } from "react";
import { Loader2, AlertCircle } from "lucide-react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import "@xterm/xterm/css/xterm.css";
import { useSessionStore } from "@/stores/session-store";

export function TerminalView() {
  const { activeSessionId, sessions } = useSessionStore();
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<Terminal | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);

  const session = sessions.find((s) => s.id === activeSessionId);

  const connectWebSocket = useCallback(() => {
    if (!activeSessionId || wsRef.current) return;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const url = `${protocol}//${window.location.host}/ws/terminal/${activeSessionId}`;
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      // Send initial size
      const term = xtermRef.current;
      if (term) {
        const resize = JSON.stringify({ cols: term.cols, rows: term.rows });
        const payload = new Uint8Array(1 + resize.length);
        payload[0] = 0x01;
        new TextEncoder().encodeInto(resize, payload.subarray(1));
        ws.send(payload);
      }
    };

    ws.onmessage = (event) => {
      const data = new Uint8Array(event.data as ArrayBuffer);
      if (data.length > 1 && data[0] === 0x00) {
        xtermRef.current?.write(data.subarray(1));
      }
    };

    ws.onclose = () => {
      wsRef.current = null;
    };

    ws.onerror = () => {
      wsRef.current = null;
    };
  }, [activeSessionId]);

  const disconnectWebSocket = useCallback(() => {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
  }, []);

  // Initialize xterm.js
  useEffect(() => {
    if (!terminalRef.current || session?.workspaceStatus !== "ready") return;

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Menlo, monospace",
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

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    // Send keystrokes to WebSocket with 0x00 prefix
    term.onData((data) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        const encoded = new TextEncoder().encode(data);
        const payload = new Uint8Array(1 + encoded.length);
        payload[0] = 0x00;
        payload.set(encoded, 1);
        ws.send(payload);
      }
    });

    // Handle resize
    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit();
    });
    resizeObserver.observe(terminalRef.current);

    term.onResize(({ cols, rows }) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        const resize = JSON.stringify({ cols, rows });
        const encoded = new TextEncoder().encode(resize);
        const payload = new Uint8Array(1 + encoded.length);
        payload[0] = 0x01;
        payload.set(encoded, 1);
        ws.send(payload);
      }
    });

    // Connect WebSocket
    connectWebSocket();

    return () => {
      resizeObserver.disconnect();
      disconnectWebSocket();
      term.dispose();
      xtermRef.current = null;
      fitAddonRef.current = null;
    };
  }, [session?.workspaceStatus, activeSessionId, connectWebSocket, disconnectWebSocket]);

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
      className="h-full w-full bg-[#0d1117]"
    />
  );
}
