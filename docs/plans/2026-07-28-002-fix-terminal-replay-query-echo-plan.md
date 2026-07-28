---
title: "fix: Stop replayed terminal output from echoing escape-sequence replies into the shell"
type: fix
status: completed
date: 2026-07-28
depth: standard
---

# fix: Stop replayed terminal output from echoing escape-sequence replies into the shell

## Summary

Every switch to the Terminal tab injects `11;rgb:0d0d/1111/1717R` (repeated) at the shell prompt. The string is xterm.js **answering** an OSC 11 background-colour query that lives permanently in the server's PTY replay buffer and is re-delivered to every freshly-mounted terminal. The fix is to frame replayed bytes distinctly from live bytes on the wire, and have the client refuse to send anything back to the PTY until replay has been fully parsed.

---

## Problem Frame

`0d0d/1111/1717` is exactly the `#0d1117` theme background configured in `src/components/terminal/TerminalView.tsx`. The bytes do not originate in the workspace — the browser generates them.

The loop:

1. Something in the remote shell emitted a terminal query (`ESC ] 11 ; ? ST` — "what is your background colour?"). Prompt frameworks, `ls` colour probes, and TUIs all do this.
2. `Session.readLoop` in `server/internal/terminal/manager.go` copies **raw** PTY bytes into a 100 KiB replay buffer. Queries are stored verbatim alongside ordinary output.
3. `src/components/layout/CenterPanel.tsx` renders `<TerminalView />` conditionally on `activeTab === "terminal"`, so every tab switch unmounts and remounts it. The `useEffect` in `TerminalView.tsx` builds a **new** `Terminal` and a **new** WebSocket each time.
4. `Session.AddClient` replays the whole buffer into that fresh xterm instance.
5. The fresh xterm cannot distinguish a replayed query from a live one. It answers via `term.onData`, which `TerminalView.tsx` forwards to PTY stdin as a `0x00` frame.
6. bash is sitting at a prompt with readline active. It discards the unrecognised `ESC ]` introducer and inserts the printable remainder into the line buffer, which is then echoed.

It recurs on every switch because the query never leaves the replay buffer. It appears doubled because two replies land per remount — React StrictMode double-invokes the effect in dev, briefly creating two xterm instances that each answer once. The trailing `R` is consistent with a second replayed query (most likely DSR `ESC [ 6 n`, whose CPR reply ends in `R`); confirming its exact source is an implementation-time detail, not a planning-time blocker.

The class of bug is broader than OSC 11. xterm.js auto-answers DA1, DA2, DSR/CPR, XTVERSION, XTGETTCAP, and the OSC 10/11/12 colour queries. Any of them sitting in a replay buffer produces the same corruption. The fix must close the class, not one instance.

**Symptom vs. corruption.** This is cosmetic-looking but is real stdin injection. A user who presses Enter without noticing runs `11;rgb:0d0d/1111/1717R` as a command. It is not exploitable — the injected bytes are self-generated and bounded — but it is data entering a shell without user intent.

---

## Requirements

- **R1** — Switching to the Terminal tab must not write anything to PTY stdin.
- **R2** — The same guarantee must hold for every path that creates a fresh terminal: page reload, a second browser tab attaching to the same session, StrictMode double-mount.
- **R3** — The fix must cover all terminal query types, not an enumerated subset.
- **R4** — Replay must keep working. The user still lands on the previous screen contents, not a blank terminal.
- **R5** — Live queries must keep working. A TUI started *after* connect (vim, htop) still gets its colour/capability answers.
- **R6** — A missing or delayed replay-boundary signal must not permanently mute the terminal.

---

## Key Technical Decisions

### KTD1 — Server marks the replay boundary; client suppresses until it arrives

**Decision.** Extend the terminal WebSocket protocol with two server→client frames: `0x02` (replay chunk) and `0x03` (replay complete). The client writes both `0x00` and `0x02` payloads to xterm, but drops everything `onData` produces until it has seen `0x03` *and* xterm has finished parsing the replayed bytes.

**Rationale.** This is correct by construction. It does not require knowing which escape sequences xterm answers, so it satisfies R3 without an enumeration that goes stale when xterm adds a query handler. The boundary is authoritative rather than inferred.

**Alternatives rejected:**

- *Frontend-only quiet window* (drop outbound data for ~N ms after connect). One file, no protocol change — but timing-based. A large replay over a slow link outlasts the window; a fast typist loses real keystrokes. Fails R3 in the tail.
- *Sanitize the replay buffer server-side* (strip queries before storing). No protocol change and server-only — but it is a denylist. Any query type not enumerated still leaks, which fails R3 directly. Retained as optional defence-in-depth (see Scope Boundaries).
- *Keep `TerminalView` permanently mounted* (CSS-hide instead of unmount). Removes the remount and is a genuine UX win, but is not a fix: reload and multi-tab still replay. Routed to follow-up work.

### KTD2 — Replay is written through an explicit `Client` interface, under the session lock

**Decision.** Change `Session.AddClient(w io.Writer)` to accept an interface carrying three operations — live write, replay write, and replay-complete — and change `Session.clients` to be keyed by that interface.

```
Client interface {
    io.Writer                    // live PTY output   → 0x00
    WriteReplay(p []byte) error  // buffered output   → 0x02
    ReplayComplete() error       // boundary marker   → 0x03
}
```

**Rationale.** The `0x03` marker must be emitted *before* the client is registered for live fan-out and *while* `s.mu` is still held. If the handler sent `0x03` after `AddClient` returned, live output could interleave between the replay and the marker, and the client would suppress a reply to a genuinely live query. Putting all three operations behind one interface lets `AddClient` keep the entire sequence ordered under a single lock acquisition.

An optional-interface type assertion (`if rs, ok := w.(replaySink)`) would preserve the existing signature, but `AddClient` has exactly one caller (`server/internal/handler/terminal.go`), so the explicit signature change is cheaper and clearer than the implicit one.

### KTD3 — Unmute on xterm's write callback, not on frame arrival

**Decision.** Receiving `0x03` does not itself unmute the client. It schedules the unmute through `term.write()`'s completion callback.

**Rationale.** This is the detail most likely to make a naive implementation fail silently. `term.write()` is **asynchronous** — xterm buffers input and parses it on a later tick. If the client sets `replayDone = true` the instant `0x03` arrives, xterm may not have parsed the replayed queries yet, and their replies will fire *after* the flag flipped. The bug survives the fix.

`@xterm/xterm` v6 (`node_modules/@xterm/xterm/typings/xterm.d.ts:1253`) exposes `write(data: string | Uint8Array, callback?: () => void): void`, and writes are processed in order. Writing a zero-length payload on `0x03` and unmuting inside its callback guarantees every preceding replay chunk has been fully parsed first.

### KTD4 — Timeout fallback so a missing `0x03` cannot brick the terminal

**Decision.** Arm a timer on WebSocket open that force-unmutes after a bounded delay (~2s) if `0x03` never arrives. Clear it when the boundary is handled.

**Rationale.** R6. A client talking to an older server binary would otherwise be permanently unable to type. Degrading to today's behaviour (occasional echoed reply) is strictly better than an unusable terminal.

### KTD5 — `0x03` is always sent, including when the replay buffer is empty

**Decision.** `AddClient` emits `ReplayComplete()` unconditionally, not only when `len(s.replay) > 0`.

**Rationale.** The first connection to a fresh PTY has an empty buffer. Without an unconditional marker, that client relies solely on the KTD4 timeout and is muted for the full fallback window — the worst case for the most common first-use path.

---

## High-Level Technical Design

### The loop as it exists today

```mermaid
sequenceDiagram
    participant Shell as Remote shell (PTY)
    participant Mgr as terminal.Session
    participant Buf as replay buffer
    participant WS as /ws/terminal
    participant Term as xterm.js (fresh instance)

    Note over Shell: earlier in the session
    Shell->>Mgr: ESC ] 11 ; ? ST   (query)
    Mgr->>Buf: appendReplay(raw bytes, query included)

    Note over Term: user switches to Terminal tab → remount
    Term->>WS: connect
    WS->>Mgr: AddClient
    Mgr->>Term: 0x00 + entire replay (query re-delivered)
    Term-->>Term: parses query, treats it as live
    Term->>WS: 0x00 + ESC ] 11 ; rgb:0d0d/1111/1717 ST
    WS->>Shell: written to PTY stdin
    Note over Shell: readline drops ESC ], echoes the rest
```

### The flow after the fix

```mermaid
sequenceDiagram
    participant Shell as Remote shell (PTY)
    participant Mgr as terminal.Session
    participant WS as /ws/terminal
    participant Term as xterm.js (fresh instance)

    Term->>WS: connect
    Note over Term: replayDone = false; outbound gate CLOSED
    WS->>Mgr: AddClient(client)

    rect rgb(30,40,55)
    Note over Mgr,Term: all three emitted under s.mu, before live registration
    Mgr->>Term: 0x02 + replay bytes
    Mgr->>Term: 0x03 (replay complete)
    end

    Term-->>Term: parses replay, generates query replies
    Term--xWS: replies DROPPED (gate closed)
    Term-->>Term: write("", cb) callback fires after parse completes
    Note over Term: replayDone = true; gate OPEN

    Shell->>Mgr: live output
    Mgr->>Term: 0x00 + data
    Term->>WS: 0x00 + real keystrokes / live query replies
    WS->>Shell: PTY stdin
```

### Wire protocol after this change

| Frame | Direction | Payload | Meaning |
| --- | --- | --- | --- |
| `0x00` | both | raw bytes | Live terminal I/O — stdout to client, stdin from client |
| `0x01` | client → server | JSON `{cols,rows}` | Resize |
| `0x02` | server → client | raw bytes | **New.** Replayed historical output. Render, but suppress responses |
| `0x03` | server → client | empty | **New.** Replay complete; responses may resume |

The `0x02`/`0x03` frames are server→client only. The client never emits them.

---

## Implementation Units

### U1. Frame replayed PTY output distinctly from live output

**Goal.** The server tells the client which bytes are history and when history ends.

**Requirements.** R1, R2, R3, R4, R5 (server half)

**Dependencies.** None

**Files:**
- `server/internal/terminal/manager.go` — modify
- `server/internal/handler/terminal.go` — modify
- `server/internal/terminal/manager_test.go` — create

**Approach.**

Introduce the `Client` interface from KTD2 in the `terminal` package and change `Session.clients` to `map[Client]bool`. Rewrite `AddClient` so that, under a single `s.mu` acquisition, it (a) writes the replay buffer via `WriteReplay` when non-empty, (b) calls `ReplayComplete()` unconditionally per KTD5, then (c) registers the client for live fan-out. A write error at any step aborts registration, matching the existing early-return behaviour.

`readLoop`'s fan-out continues to use the plain `io.Writer` half — live output framing is unchanged.

In the handler, generalise `wsWriter` to carry its frame prefix, and add `WriteReplay`/`ReplayComplete` methods that emit `0x02` and `0x03`. A single `wsWriter` value can serve all three operations; a per-frame prefix field is not required if the methods construct their own prefix byte.

Update the wire-protocol doc comment at the top of `HandleTerminalWebSocket` to document `0x02` and `0x03`, including the server→client-only direction constraint.

**Patterns to follow.**
- `wsWriter` (`server/internal/handler/terminal.go:120`) is the existing framing shim — extend it rather than introducing a parallel type.
- Locking discipline in `readLoop`/`AddClient` (`manager.go:86`, `manager.go:134`) — hold `s.mu` across the write-and-mutate sequence; do not release between replay and registration.
- Go test layout follows `server/internal/handler/*_test.go` (table-free, standard library `testing`, no external assertion library).

**Test scenarios.**
- `AddClient` with a non-empty replay buffer calls `WriteReplay` with the buffer contents, then `ReplayComplete`, in that order, and only then makes the client visible to fan-out.
- `AddClient` with an **empty** replay buffer still calls `ReplayComplete` exactly once and calls `WriteReplay` zero times.
- Output produced by `readLoop` after registration reaches the client through the live `Write` path, never through `WriteReplay`.
- A client whose `WriteReplay` returns an error is not registered for live fan-out, and a subsequent `readLoop` write does not reach it.
- A client whose `ReplayComplete` returns an error is not registered for live fan-out.
- Two clients added in sequence each receive their own full replay and their own `ReplayComplete`; the second client's replay includes output that arrived after the first client attached.
- `appendReplay` still trims to `replayBufferSize` and preserves the most recent bytes (guard against regressing the existing ring behaviour while editing adjacent code).

**Verification.** `cd server && go test ./internal/terminal/... ./internal/handler/...` passes. `go build ./...` succeeds — the `AddClient` signature change has exactly one call site, so a clean build is meaningful evidence the change is complete.

---

### U2. Suppress client→PTY writes until replay is fully parsed

**Goal.** A freshly-mounted xterm renders history without answering any of it.

**Requirements.** R1, R2, R3, R5, R6

**Dependencies.** U1

**Files:**
- `src/components/terminal/TerminalView.tsx` — modify
- `src/components/terminal/TerminalView.test.tsx` — create

**Approach.**

Add a `replayDone` flag scoped to the effect (a plain `let`, alongside the existing `disposed`), initialised `false`.

In `onData`, return early while `replayDone` is false. Leave `onResize` alone — resize frames are not query replies and are safe to send during replay.

Extend `ws.onmessage` to dispatch on the leading byte:
- `0x00` → `term.write(payload)` (unchanged)
- `0x02` → `term.write(payload)` — rendered identically; the frame type only governs the outbound gate
- `0x03` → `term.write(new Uint8Array(0), () => { replayDone = true })` per KTD3, and clear the fallback timer

Arm the KTD4 fallback timer in `ws.onopen`, and clear it in both the `0x03` path and the effect cleanup so a unmounted terminal cannot leave a live timer.

Guard every `replayDone` assignment with the existing `disposed` check, consistent with how `onmessage` already early-returns.

Note the current `data.length > 1` condition on the `0x00` branch — `0x03` carries an empty payload, so the new branch must not inherit that length guard.

**Technical design** *(directional guidance, not implementation specification)*:

```
let replayDone = false
let unmuteTimer = setTimeout(...)   // armed on open, KTD4

term.onData(d => {
  if (!replayDone) return           // drop query replies generated by replay
  ws.send(0x00 + d)
})

ws.onmessage = e => {
  if (disposed) return
  switch (frame[0]) {
    case 0x00: term.write(body); break
    case 0x02: term.write(body); break            // history: render, stay muted
    case 0x03: clearTimeout(unmuteTimer)
               term.write(EMPTY, () => { if (!disposed) replayDone = true })
  }
}
```

**Patterns to follow.**
- The existing `disposed` guard convention in the same effect (`TerminalView.tsx:98`, `TerminalView.tsx:87`) — every async callback checks it before touching terminal state.
- The existing binary framing helper shape used by `onData`/`onResize` (`TerminalView.tsx:52`, `TerminalView.tsx:63`).
- Component test setup follows `src/components/chat/MessageBubble.test.tsx` and the jsdom harness in `src/test/setup-dom.ts`.

**Test scenarios.**

These require stubbing `WebSocket` and asserting on what the component *sends*. Assert on outbound frames rather than on internal flags, so the tests survive refactors of the gating mechanism.

- Data emitted by `onData` before any `0x03` is received produces **zero** outbound `0x00` frames.
- After a `0x03` frame is delivered and xterm's write callback has flushed, data emitted by `onData` produces an outbound `0x00` frame with the expected payload — i.e. normal typing is restored (R5 guard).
- A `0x02` frame's payload is rendered to the terminal (not silently dropped) — replay still paints (R4 guard).
- A `0x03` frame arriving while xterm still has unparsed `0x02` content does not unmute until the parse completes. Drive this by asserting the unmute is observed only after the injected write callback fires, not synchronously on frame receipt — this is the KTD3 regression that a naive implementation would ship broken.
- The specific reported case: a `0x02` payload containing `ESC ] 11 ; ? BEL` produces **no** outbound frame containing `rgb:`. This is the literal bug reproduction and should read as such.
- A `0x02` payload containing DSR (`ESC [ 6 n`) produces no outbound CPR frame — proves the fix is class-wide (R3) rather than OSC-specific.
- With no `0x03` ever delivered, outbound data is still sent after the fallback interval elapses (R6). Use fake timers.
- Resize frames (`0x01`) are still sent during replay — the gate is scoped to `onData` only.
- Effect cleanup clears the fallback timer; unmounting before `0x03` arrives leaves no pending timer.

**Verification.** `npm test` passes and `npx tsc -b --force` is clean. End-to-end: with a session whose shell has emitted a colour query, switch away from and back to the Terminal tab repeatedly — the prompt stays clean, prior scrollback still renders, and typing works immediately. Then start a TUI (e.g. `vim`) and confirm it renders with correct colours, proving live queries are still answered (R5).

---

## System-Wide Impact

**Deploy skew.** The frontend is served by the same Go binary, so the skew window is limited to a browser tab left open across a deploy.

- *Old client, new server.* The old `onmessage` only handles `0x00`, so `0x02` replay frames are ignored and the terminal reconnects blank. Degradation, not corruption; a refresh fixes it.
- *New client, old server.* No `0x02`/`0x03` ever arrives. The KTD4 timer unmutes after the fallback interval and replay arrives as `0x00`, i.e. exactly today's behaviour. This is precisely why KTD4 is not optional.

**Multi-client sessions.** One PTY fans out to N clients. Each attaching client gets its own replay and its own `0x03`; already-attached clients see nothing new. No cross-client interference.

**Accepted trade-off.** Keystrokes typed during the replay window are dropped. The window is one bounded write (≤100 KiB) over a local WebSocket plus one xterm parse tick — sub-frame in practice, and strictly better than today, where those same keystrokes land in a line buffer already polluted with escape-sequence garbage.

---

## Scope Boundaries

**In scope.** The replay→reply→stdin loop on `/ws/terminal`, on every path that constructs a fresh xterm instance.

### Deferred to Follow-Up Work

- **Keep `TerminalView` mounted across tab switches** (CSS-hide instead of conditional render in `src/components/layout/CenterPanel.tsx`). Removes the remount entirely — preserves scrollback, avoids reflow, drops a reconnect per switch. A real UX improvement, but not a fix: reload and multi-tab attach still replay. Worth doing on its own merits afterwards.
- **Sanitize queries out of the replay buffer** as defence-in-depth. Redundant once the boundary fix lands; only becomes interesting if a future non-xterm client attaches.
- **Per-client PTY sizing.** Multiple clients on one PTY currently fight over `Setsize`. Pre-existing, unrelated, out of scope.

### Not Doing

- Broader terminal-session rework — scrollback persistence, PTY lifecycle, reconnect semantics.
- Converging the browser terminal onto the SSH proxy path (noted as a v2 cleanup in `CLAUDE.md`).

---

## Open Questions (deferred to implementation)

- **Exact provenance of the trailing `R`.** Almost certainly a second replayed query — DSR/CPR is the strongest candidate. Confirm by logging the replay buffer once during implementation. It does not gate the fix: the boundary approach suppresses it regardless of which query produced it, and U2 has a test scenario covering DSR specifically.
- **Fallback interval value.** ~2s is a starting point. Tune once the real replay-flush latency is observable; the only constraint is that it comfortably exceeds a 100 KiB write plus one xterm parse tick.
- **Whether `wsWriter` needs a prefix field or three methods.** Shape decision best made against the actual code; either satisfies KTD2.

---

## Sources

- Origin implementation: `docs/plans/2026-05-08-feat-terminal-devpod-connection-plan.md` — established the `0x00`/`0x01` framing this plan extends.
- `node_modules/@xterm/xterm/typings/xterm.d.ts:1253` — `write(data, callback)` signature underpinning KTD3.
- `CLAUDE.md` — "Terminal vs Open-in-VS-Code divergence" records that the browser terminal path is `devpod ssh` + PTY, distinct from the SSH proxy.
