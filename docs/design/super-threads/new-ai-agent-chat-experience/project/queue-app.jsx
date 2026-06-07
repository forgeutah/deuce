/* Super Threads — Queue prototype.
   Global per-agent threads · auto-promoting queue · inline anchored task cards ·
   Claude Code-style live action log in the thread. */
const { useState, useEffect, useRef, useReducer } = React;
const { Icon } = window.STShell;

const AGS = {
  coder:    { id: "coder",    name: "Coder",    color: "#58a6ff", role: "Writes & modifies code" },
  reviewer: { id: "reviewer", name: "Reviewer", color: "#BE8FFF", role: "Reviews diffs critically" },
};
const U = {
  clint: { name: "Clint Berry", seed: "Clint" },
  sarah: { name: "Sarah Chen",  seed: "Sarah" },
  mike:  { name: "Mike Rodriguez", seed: "Mike" },
};
const ME = U.clint;

const clock = () => new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
let _id = 100; const nid = () => "t" + (++_id);
let _mid = 100; const nmid = () => "c" + (++_mid);
const firstName = (n) => n.split(" ")[0];
const stripMention = (t) => t.replace(/@\w+/, "").replace(/^[\s,]+/, "").trim();

/* ── diffs + command output ───────────────────────────────────── */
const DIFF_VALIDATE = [
  ["ctx", "@@ func Validate(token string) error"],
  ["add", "+  claims, err := ParseClaims(token)"],
  ["add", "+  if claims.ExpiresAt.Before(time.Now()) {"],
  ["add", "+    return ErrTokenExpired"],
  ["add", "+  }"],
  ["ctx", "   return nil"],
];
const DIFF_REFRESH = [
  ["ctx", "@@ func Refresh(w http.ResponseWriter, claims *Claims)"],
  ["add", "+  newRT, err := RotateRefresh(claims.Subject)"],
  ["add", "+  if err != nil { return err }"],
  ["add", "+  setRefreshCookie(w, newRT)"],
  ["ctx", "   return nil"],
];
const DIFF_GENERIC = [
  ["ctx", "@@ internal/auth"],
  ["add", "+  if err := apply(); err != nil {"],
  ["add", "+    return fmt.Errorf(\"apply: %w\", err)"],
  ["add", "+  }"],
];
const DIFF_TEST = [
  ["ctx", "@@ new file: validate_test.go"],
  ["add", "+func TestValidateExpired(t *testing.T) {"],
  ["add", "+  err := Validate(expiredToken())"],
  ["add", "+  require.ErrorIs(t, err, ErrTokenExpired)"],
  ["add", "+}"],
];
const OK_BUILD = [["ok", "ok  \tforge-api/internal/auth"]];
const TEST_PASS = [["ctx", "=== RUN   TestRefresh"], ["ok", "--- PASS: TestRefresh (0.00s)"], ["ok", "PASS"], ["ctx", "ok  forge-api/internal/auth  0.004s"]];
const TEST_PASS6 = [["ok", "--- PASS: TestValidate (0.00s)"], ["ok", "--- PASS: TestValidateExpired (0.00s)"], ["ok", "--- PASS: TestValidateInvalid (0.00s)"], ["ok", "PASS  6/6"]];

/* ── action streams (what the agent does, Claude Code-style) ──── */
function actionsFor(prompt) {
  const p = prompt.toLowerCase();
  if (p.includes("refresh") || p.includes("rotat") || p.includes("cookie")) return [
    { tool: "Read", arg: "internal/auth/refresh.go", ms: 1500 },
    { tool: "Grep", arg: "RotateRefresh", note: "2 matches", ms: 1100 },
    { tool: "Edit", arg: "internal/auth/refresh.go", stat: "+16 −2", diff: DIFF_REFRESH, ms: 2600 },
    { tool: "Bash", arg: "go build ./internal/auth/…", out: OK_BUILD, ms: 1600 },
    { tool: "Bash", arg: "go test ./internal/auth/…", out: TEST_PASS, ms: 2100 },
  ];
  if (p.includes("test")) return [
    { tool: "Read", arg: "internal/auth/validate.go", ms: 1400 },
    { tool: "Think", text: "planning cases: valid, expired, malformed, empty input", ms: 1300 },
    { tool: "Write", arg: "internal/auth/validate_test.go", stat: "+38 −0", diff: DIFF_TEST, ms: 2600 },
    { tool: "Bash", arg: "go test ./internal/auth/… -run TestValidate", out: TEST_PASS6, ms: 2200 },
  ];
  if (p.includes("valid") || p.includes("expir") || p.includes("token")) return [
    { tool: "Read", arg: "internal/auth/validate.go", ms: 1500 },
    { tool: "Edit", arg: "internal/auth/validate.go", stat: "+12 −3", diff: DIFF_VALIDATE, ms: 2500 },
    { tool: "Bash", arg: "go build ./internal/auth/…", out: OK_BUILD, ms: 1500 },
    { tool: "Bash", arg: "go test ./internal/auth/…", out: TEST_PASS, ms: 2000 },
  ];
  return [
    { tool: "Read", arg: "internal/auth/auth.go", ms: 1400 },
    { tool: "Edit", arg: "internal/auth/auth.go", stat: "+11 −2", diff: DIFF_GENERIC, ms: 2400 },
    { tool: "Bash", arg: "go build ./internal/auth/…", out: OK_BUILD, ms: 1500 },
    { tool: "Bash", arg: "go test ./internal/auth/…", out: TEST_PASS, ms: 2000 },
  ];
}
const reviewerActions = () => [
  { tool: "Read", arg: "internal/auth/validate.go", ms: 1500 },
  { tool: "Read", arg: "internal/auth/middleware.go", ms: 1400 },
  { tool: "Think", text: "checking error handling, edge cases and test coverage", ms: 1900 },
];
function replyFor(prompt) {
  const p = prompt.toLowerCase();
  if (p.includes("refresh") || p.includes("rotat") || p.includes("cookie")) return "Refresh tokens now rotate on every use and the old token is revoked. Build + tests green.";
  if (p.includes("test")) return "Tests added and passing — covered valid, expired, malformed and empty inputs. 6/6 green.";
  if (p.includes("valid") || p.includes("expir") || p.includes("token")) return "Done — Validate() parses the JWT claims and rejects expired tokens with ErrTokenExpired. Build passes.";
  return "Done — applied the change, ran the build and the suite. Everything's green.";
}
const sumMs = (acts) => acts.reduce((s, a) => s + a.ms, 0);
const deriveWork = (acts) => { const e = [...(acts || [])].reverse().find((a) => a.diff); return e ? { file: e.arg, stat: e.stat } : null; };

/* turn a queued/new task into a running one with its action stream attached */
function startTask(task, agId) {
  const actions = agId === "reviewer" ? reviewerActions() : actionsFor(task.prompt);
  return { ...task, state: "running", ts: clock(), startedAt: Date.now(), actions, dur: sumMs(actions) };
}
/* index of the action currently in flight (=== length when all done) */
function revealIdx(task, now) {
  const acts = task.actions || []; let acc = 0; const el = now - (task.startedAt || now);
  for (let i = 0; i < acts.length; i++) { acc += acts[i].ms; if (el < acc) return i; }
  return acts.length;
}

/* ── seed ─────────────────────────────────────────────────────── */
function seed() {
  const t1act = actionsFor("token expiration validate");
  const t1 = { id: "t1", who: U.clint, anchorId: "m1", prompt: "@Coder add token expiration checking to the Validate function", state: "done", ts: "3:38 PM",
    actions: t1act, reply: replyFor("validate"), work: deriveWork(t1act) };
  const t2 = startTask({ id: "t2", who: U.sarah, anchorId: "m2", prompt: "@Coder while you're in there, rotate refresh tokens on each use" }, "coder");
  const r1act = reviewerActions();
  const r1 = { id: "r1", who: U.sarah, anchorId: "m4", prompt: "@Reviewer take a pass on the expiration diff", state: "done", ts: "3:55 PM",
    actions: r1act, reply: "Looks correct. Two small notes: use errors.Is() for the expiry comparison, and add context to the parse error. Token logic itself is solid.", work: null };
  return { coder: [t1, t2], reviewer: [r1] };
}
const SEED_CHANNEL = [
  { id: "m1", who: U.clint, text: "Let's get auth buttoned up. @Coder add token expiration checking to the Validate function.", ts: "3:38 PM" },
  { id: "m2", who: U.sarah, text: "Nice. @Coder while you're in there, rotate refresh tokens on each use.", ts: "3:52 PM" },
  { id: "m3", who: U.mike,  text: "I'll take the rate-limiting in a separate session so it doesn't collide.", ts: "3:54 PM" },
  { id: "m4", who: U.sarah, text: "Good progress. @Reviewer already took a pass on the expiration diff — notes below.", ts: "3:55 PM" },
];

/* ── small atoms ──────────────────────────────────────────────── */
function parseMentions(text) {
  const out = [];
  (text.match(/@(\w+)/g) || []).forEach((m) => { const k = m.slice(1).toLowerCase(); if (AGS[k] && !out.includes(k)) out.push(k); });
  return out;
}
function Mentioned({ text, color }) {
  return text.split(/(@\w+)/g).map((p, i) => /^@\w+$/.test(p)
    ? <span key={i} className="mention" style={{ color: color || "var(--accent)", fontWeight: 600 }}>{p}</span>
    : <React.Fragment key={i}>{p}</React.Fragment>);
}
const Avh = ({ seed, size = 24 }) => <img className="avh" src={`https://api.dicebear.com/9.x/avataaars/svg?seed=${seed}`} style={{ width: size, height: size, borderRadius: "50%", flexShrink: 0 }} alt="" />;
const AvA = ({ ag, size = 22 }) => <div className="av" style={{ background: ag.color, width: size, height: size, fontSize: size * 0.42, borderRadius: 5, display: "flex", alignItems: "center", justifyContent: "center", color: "#06121f", fontWeight: 700, flexShrink: 0 }}>{ag.name[0]}</div>;
const Tdots = ({ c }) => <div style={{ display: "flex", gap: 4 }}>{[0, .2, .4].map((d, i) => <span key={i} className="tdot" style={{ background: c, animationDelay: `${d}s` }} />)}</div>;
const Pre = ({ lines }) => <pre>{lines.map((l, i) => <div key={i} className={l[0] === "add" ? "diff-add" : "diff-ctx"}>{l[1]}</div>)}</pre>;
const TOOL_ICON = { Read: "FileText", Grep: "Search", Edit: "Pencil", Write: "FilePlus", Bash: "SquareTerminal", Think: "Sparkles" };

/* ── Action log (the Claude Code-style stream) ────────────────── */
function ActionItem({ a, status }) {
  if (a.tool === "Think") {
    return (
      <div className="q-act think">
        <div className="q-act-row">
          <span className="q-act-ic"><Icon name="Sparkles" /></span>
          <span><span className="tool" style={{ fontStyle: "normal" }}>Thinking</span> — {a.text}</span>
          {status === "run" && <span className="q-act-stat run"><Icon name="Loader2" /></span>}
        </div>
      </div>
    );
  }
  return (
    <div className="q-act">
      <div className="q-act-row">
        <span className="q-act-ic"><Icon name={TOOL_ICON[a.tool] || "Dot"} /></span>
        <span><span className="tool">{a.tool}</span><span className="paren">(</span><span className="arg">{a.arg}</span><span className="paren">)</span>{a.note && <span className="note">{a.note}</span>}</span>
        <span className={`q-act-stat ${status}`}><Icon name={status === "done" ? "Check" : "Loader2"} /></span>
      </div>
      {status === "done" && a.diff && (
        <div className="q-detail">
          <div className="top"><Icon name="Pencil" />{a.arg}<span className="stat">{a.stat}</span></div>
          <Pre lines={a.diff} />
        </div>
      )}
      {status === "done" && a.out && (
        <pre className="q-out">{a.out.map((o, i) => <div key={i} className={o[0]}>{o[1]}</div>)}</pre>
      )}
    </div>
  );
}
function ActionLog({ task, now }) {
  const acts = task.actions || [];
  const idx = task.state === "done" ? acts.length : revealIdx(task, now);
  return (
    <div className="q-actions">
      {acts.map((a, i) => {
        const status = (task.state === "done" || i < idx) ? "done" : i === idx ? "run" : "pending";
        return status === "pending" ? null : <ActionItem key={i} a={a} status={status} />;
      })}
    </div>
  );
}

/* ── Inline anchored task card ────────────────────────────────── */
function TaskCard({ ag, task, qpos, now, onOpen }) {
  const st = task.state;
  const acts = task.actions || [];
  const cur = acts[Math.min(revealIdx(task, now), acts.length - 1)] || { tool: "", arg: "" };
  return (
    <div className={`tc ${st}`} style={{ "--ac": ag.color }} onClick={onOpen}>
      <div className="tc-inner" key={st}>
        {st === "running" && (
          <>
            <div className="tc-hd">
              <AvA ag={ag} size={22} /><span className="nm">{ag.name}</span><span className="role">· session thread</span>
              <span className="spacer" />
              <span className="q-badge working"><Icon name="Loader" size={11} className="lucide spin" />Working</span>
              <span className="chev"><Icon name="ChevronRight" /></span>
            </div>
            <div className="tc-live"><span className="pip" /><span className="tool">{cur.tool === "Think" ? "Thinking" : cur.tool}</span><span className="arg">{cur.tool === "Think" ? "" : cur.arg}</span></div>
            <div className="tc-typing"><Tdots c={ag.color} /><span className="lbl">{ag.name} is working — open to watch</span></div>
          </>
        )}
        {st === "queued" && (
          <div className="tc-q">
            <AvA ag={ag} size={22} />
            <div className="info">
              <div className="l1"><Icon name="Clock" size={12} />Queued for {ag.name} · waiting for current task</div>
              <div className="l2">{stripMention(task.prompt)}</div>
            </div>
            <span className="pos">#{qpos}</span>
          </div>
        )}
        {st === "done" && (
          <div className="tc-d">
            <AvA ag={ag} size={18} /><span className="ck"><Icon name="Check" size={13} /></span>
            <span className="l"><b>{ag.name}</b> {task.reply}</span>
            {task.work && <span className="stat">{task.work.stat}</span>}
            <span className="chev" style={{ marginLeft: 2 }}><Icon name="ChevronRight" size={14} style={{ color: "var(--fg-subtle)" }} /></span>
          </div>
        )}
      </div>
    </div>
  );
}

/* ── Drawer: global thread with full action history ───────────── */
function Turn({ ag, task, now, qpos }) {
  const [open, setOpen] = useState(false);
  return (
    <div className={`q-turn ${task.state}`}>
      <div className="q-req">
        <div className="hd"><Avh seed={task.who.seed} /><span className="nm">{task.who.name}</span><span className="ts">{task.ts}</span></div>
        <div className="bd"><Mentioned text={task.prompt} color={ag.color} /></div>
      </div>

      {task.state === "running" && (
        <div className="q-resp" style={{ "--ac": ag.color }}>
          <div className="q-typingline"><AvA ag={ag} size={22} /><Tdots c={ag.color} /><span className="lbl">{ag.name} is working…</span></div>
          <ActionLog task={task} now={now} />
        </div>
      )}

      {task.state === "done" && (
        <div className="q-resp" style={{ "--ac": ag.color }}>
          <div className="agent-line"><AvA ag={ag} size={22} /><span className="nm">{ag.name}</span><span className="ts">{task.ts}</span></div>
          <div className="q-actsum" onClick={(e) => { e.stopPropagation(); setOpen(!open); }}>
            <Icon name={open ? "ChevronDown" : "ChevronRight"} size={13} />
            <span className="ct">{(task.actions || []).length} actions</span>
            {task.work && <>· <span className="stat">{task.work.file} {task.work.stat}</span></>}
          </div>
          {open && <ActionLog task={task} now={now} />}
          <div className="bd">{task.reply}</div>
        </div>
      )}

      {task.state === "queued" && (
        <div className="q-queued-card">
          <AvA ag={ag} size={22} />
          <div className="info">
            <div className="l1"><Icon name="Clock" size={12} />Queued · position {qpos}</div>
            <div className="l2">Waiting for {ag.name}'s current task to finish, then starts automatically.</div>
          </div>
        </div>
      )}
    </div>
  );
}

function Drawer({ ag, tasks, now, onClose, onSend }) {
  const [val, setVal] = useState("");
  const bodyRef = useRef(null);
  const running = tasks.find((t) => t.state === "running");
  useEffect(() => { const el = bodyRef.current; if (el) el.scrollTop = el.scrollHeight; }, [tasks.length, tasks.map((t) => t.state).join(""), now]);
  let qp = 0;
  const send = () => { const t = val.trim(); if (!t) return; onSend(ag.id, t); setVal(""); };
  return (
    <div className="q-drawer" style={{ "--ac": ag.color }}>
      <div className="q-drawer-hd">
        <AvA ag={ag} size={26} />
        <div style={{ minWidth: 0 }}>
          <div className="nm">{ag.name}</div>
          <div className="sub">{running ? <><span style={{ width: 7, height: 7, borderRadius: "50%", background: ag.color, display: "inline-block" }} />Working · global thread</> : "Idle · global thread"}</div>
        </div>
        <button className="x" onClick={onClose}><Icon name="X" /></button>
      </div>
      <div className="q-thread" ref={bodyRef}>
        <div className="q-thread-foot" style={{ paddingTop: 0 }}>Start of thread with {ag.name}</div>
        {tasks.map((t) => { if (t.state === "queued") qp += 1; return <Turn key={t.id} ag={ag} task={t} now={now} qpos={t.state === "queued" ? qp : 0} />; })}
      </div>
      <div className="q-drawer-composer">
        <div className="row">
          <textarea rows={1} value={val} placeholder={`Reply to ${ag.name}…`} onChange={(e) => setVal(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } }} />
          <button className="send" onClick={send}><Icon name="SendHorizontal" /></button>
        </div>
      </div>
    </div>
  );
}

const { MiniSidebar } = window.STShell;

/* ── App ──────────────────────────────────────────────────────── */
function App() {
  const [threads, setThreads] = useState(seed);
  const [channel, setChannel] = useState(SEED_CHANNEL);
  const [open, setOpen] = useState("coder");
  const [input, setInput] = useState("");
  const [, force] = useReducer((x) => x + 1, 0);
  const scheduled = useRef(new Set());
  const timers = useRef([]);
  const chanRef = useRef(null);
  const now = Date.now();

  useEffect(() => { const iv = setInterval(force, 500); return () => clearInterval(iv); }, []);

  /* schedule completion of running tasks */
  useEffect(() => {
    for (const a of Object.keys(AGS)) {
      const r = threads[a].find((t) => t.state === "running");
      if (r && !scheduled.current.has(r.id)) {
        scheduled.current.add(r.id);
        const remaining = Math.max(1500, (r.dur || 8000) - (Date.now() - (r.startedAt || Date.now())));
        timers.current.push(setTimeout(() => complete(a, r.id), remaining));
      }
    }
  }, [threads]);

  function promoteInPlace(list, agId) {
    if (!list.some((t) => t.state === "running")) {
      const qi = list.findIndex((t) => t.state === "queued");
      if (qi >= 0) list[qi] = startTask(list[qi], agId);
    }
    return list;
  }
  function complete(agId, taskId) {
    setThreads((prev) => {
      const list = prev[agId].map((t) => t.id === taskId
        ? { ...t, state: "done", ts: clock(), reply: replyFor(t.prompt), work: deriveWork(t.actions) }
        : t);
      return { ...prev, [agId]: promoteInPlace(list, agId) };
    });
  }
  function enqueue(agId, text, anchorId) {
    const task = { id: nid(), who: ME, anchorId, prompt: text, state: "queued", ts: clock() };
    setThreads((p) => ({ ...p, [agId]: promoteInPlace([...p[agId], task], agId) }));
  }
  function channelSend() {
    const text = input.trim(); if (!text) return;
    const mentions = parseMentions(text);
    const mid = nmid();
    setChannel((c) => [...c, { id: mid, who: ME, text, ts: clock() }]);
    mentions.forEach((m) => enqueue(m, text, mid));
    setInput("");
    if (mentions.length) setOpen(mentions[0]);
  }
  function drawerSend(agId, text) {
    const mid = nmid();
    setChannel((c) => [...c, { id: mid, who: ME, text, ts: clock() }]);
    enqueue(agId, text, mid);
  }
  function reset() {
    timers.current.forEach(clearTimeout); timers.current = []; scheduled.current = new Set();
    _id = 100; _mid = 100; setThreads(seed()); setChannel(SEED_CHANNEL); setOpen("coder"); setInput("");
  }

  useEffect(() => { const el = chanRef.current; if (el) el.scrollTop = el.scrollHeight; }, [channel.length]);

  /* queue positions + anchor map */
  const qpos = {};
  for (const a of Object.keys(AGS)) { let p = 0; threads[a].forEach((t) => { if (t.state === "queued") { p += 1; qpos[t.id] = p; } }); }
  const byAnchor = {};
  for (const a of Object.keys(AGS)) threads[a].forEach((t) => { if (t.anchorId) (byAnchor[t.anchorId] = byAnchor[t.anchorId] || []).push({ ag: AGS[a], task: t }); });

  const openAg = open ? AGS[open] : null;

  return (
    <div className="q-app">
      <MiniSidebar />
      <div className="q-center">
        <button className="q-reset" onClick={reset}><Icon name="RotateCcw" size={12} />Reset demo</button>
        <div className="st-cphead" style={{ borderBottom: "1px solid var(--border-muted)", padding: "10px 16px" }}>
          <div className="st-cptitle"><Icon name="Hash" /><span className="h">auth-module</span></div>
          <span className="st-cpdesc">JWT validation and refresh-token flow for the v2 API</span>
        </div>
        <div className="st-tabbar"><button className="st-tab active"><Icon name="MessageSquare" />Chat</button><button className="st-tab"><Icon name="FileText" />Plan</button><button className="st-tab"><Icon name="FolderTree" />Files</button><button className="st-tab"><Icon name="Terminal" />Terminal</button></div>

        <div className="q-channel" ref={chanRef}>
          {channel.map((it) => (
            <React.Fragment key={it.id}>
              <div className="st-msg">
                <div className="hd"><Avh seed={it.who.seed} /><span className="nm">{it.who.name}</span><span className="ts">{it.ts}</span></div>
                <div className="bd"><Mentioned text={it.text} /></div>
              </div>
              {(byAnchor[it.id] || []).map(({ ag, task }) => (
                <TaskCard key={task.id} ag={ag} task={task} qpos={qpos[task.id]} now={now} onOpen={() => setOpen(ag.id)} />
              ))}
            </React.Fragment>
          ))}
          <div style={{ height: 8 }} />
        </div>

        <div className="q-composer">
          <div className="q-mentionbar">
            <span className="lbl">Mention an agent:</span>
            {Object.values(AGS).map((a) => (
              <button key={a.id} className="q-chip" style={{ background: a.color }}
                onClick={() => setInput((v) => (v.includes(`@${a.name}`) ? v : (v ? v + " " : "") + `@${a.name} `))}>@{a.name}</button>
            ))}
          </div>
          <div className="q-row">
            <textarea rows={1} value={input} placeholder="Message (@ to mention an agent — try @Coder while it's working)"
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); channelSend(); } }} />
            <button className="q-send" disabled={!input.trim()} onClick={channelSend}><Icon name="SendHorizontal" /></button>
          </div>
        </div>
      </div>

      {openAg && <Drawer ag={openAg} tasks={threads[open]} now={now} onClose={() => setOpen(null)} onSend={drawerSend} />}
    </div>
  );
}

function boot() { ReactDOM.createRoot(document.getElementById("root")).render(<App />); }
if (window.LucideIcons) boot(); else window.addEventListener("lucide-ready", boot, { once: true });
