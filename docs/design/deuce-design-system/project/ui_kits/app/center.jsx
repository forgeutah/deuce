/* CenterPanel + Chat / Plan / Files / Terminal / Logs views. Shared via window. */
const { useState: useStateC, useRef: useRefC, useEffect: useEffectC } = React;

/* ── Chat ─────────────────────────────────────────────────────── */
function Expandable({ item }) {
  const [open, setOpen] = useStateC(false);
  return (
    <div className="expand">
      <button className="expand-btn" onClick={() => setOpen(!open)}>{open ? "Hide" : "Show"} {item.title}</button>
      {open && (
        <div className="expand-box">
          <div className="expand-sum">{item.summary}</div>
          <pre>{item.lines.map((l, i) => <div key={i} className={l[0] === "add" ? "diff-add" : l[0] === "ctx" ? "diff-ctx" : ""}>{l[1]}</div>)}</pre>
        </div>
      )}
    </div>
  );
}

function Message({ msg, author, showHeader }) {
  const isAgent = msg.authorType === "agent";
  const style = isAgent && author ? { borderLeftColor: author.color, background: `${author.color}08` } : undefined;
  return (
    <div className={`msg ${isAgent ? "agent" : ""}`} style={style}>
      {showHeader && (
        <div className="msg-hd">
          {isAgent && author
            ? <div className="av" style={{ background: author.color }}>{author.name[0]}</div>
            : <img className="avh" src={author?.avatar} alt="" />}
          <span className="msg-name">{author?.name}</span>
          <span className="msg-ts">{clockTime(msg.createdAt)}</span>
        </div>
      )}
      <div className="msg-body" style={!showHeader ? { paddingLeft: 36 } : undefined}>
        {renderContent(msg.content)}
        {msg.expandable?.map((it, i) => <Expandable key={i} item={it} />)}
      </div>
    </div>
  );
}

function TypingIndicator({ agent, onStop }) {
  return (
    <div className="typing">
      <div className="typing-row">
        <div className="av" style={{ background: agent.color }}>{agent.name[0]}</div>
        <span className="typing-name">{agent.name} is working</span>
        <div style={{ display: "flex", gap: 4 }}>
          <span className="tdot" style={{ animationDelay: "0s" }} />
          <span className="tdot" style={{ animationDelay: ".2s" }} />
          <span className="tdot" style={{ animationDelay: ".4s" }} />
        </div>
        <button className="stop" title="Stop agent" onClick={onStop}><Icon name="Square" size={12} /></button>
      </div>
    </div>
  );
}

function ChatView({ session, messages, participants, thinking, onSend, onStop }) {
  const [input, setInput] = useStateC("");
  const scrollRef = useRefC(null);
  useEffectC(() => { const el = scrollRef.current; if (el) el.scrollTop = el.scrollHeight; }, [messages.length, thinking.length, session.id]);

  const findAuthor = (m) => m.authorType === "agent" ? session.agents.find((a) => a.id === m.authorId) : participants.find((p) => p.id === m.authorId);
  const readOnly = session.status !== "active";

  const send = () => { if (input.trim() && !readOnly) { onSend(input.trim()); setInput(""); } };

  return (
    <div className="chat">
      <div className="chat-scroll" ref={scrollRef}>
        {messages.length === 0 && (
          <div className="chat-empty">
            <Icon name="Bot" className="lucide" />
            <h3>Start a conversation</h3>
            <p>@mention an agent to get started.
              <span style={{ display: "block", marginTop: 8 }}>
                {session.agents.map((a) => (
                  <button key={a.id} className="mention-chip" style={{ background: a.color }} onClick={() => setInput(`@${a.name} `)}>@{a.name}</button>
                ))}
              </span>
            </p>
          </div>
        )}
        {messages.map((m, i) => {
          const prev = messages[i - 1];
          const showHeader = !prev || prev.authorId !== m.authorId || (new Date(m.createdAt) - new Date(prev.createdAt) > 300000);
          return <Message key={m.id} msg={m} author={findAuthor(m)} showHeader={showHeader} />;
        })}
        {thinking.map((id) => {
          const a = session.agents.find((x) => x.id === id);
          return a ? <TypingIndicator key={id} agent={a} onStop={() => onStop(id)} /> : null;
        })}
        <div />
      </div>
      <div className="composer">
        {readOnly ? (
          <div className="readonly">{session.status === "paused" ? "Session is paused" : "Session is archived"}</div>
        ) : (
          <div className="composer-row">
            <textarea value={input} rows={1} placeholder="Message (@ to mention an agent)"
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(); } }} />
            <button className="send" disabled={!input.trim()} onClick={send}><Icon name="SendHorizontal" /></button>
          </div>
        )}
      </div>
    </div>
  );
}

/* ── Plan ─────────────────────────────────────────────────────── */
function renderPlan(content) {
  const out = []; const lines = content.split("\n"); let list = [];
  const flush = () => { if (list.length) { out.push(<ul key={"u" + out.length}>{list}</ul>); list = []; } };
  const inline = (t) => t.split(/(`[^`]+`)/g).map((p, i) => p.startsWith("`") ? <code key={i}>{p.slice(1, -1)}</code> : <React.Fragment key={i}>{p}</React.Fragment>);
  lines.forEach((ln, i) => {
    if (ln.startsWith("## ")) { flush(); out.push(<h2 key={i}>{ln.slice(3)}</h2>); }
    else if (ln.startsWith("# ")) { flush(); out.push(<h1 key={i}>{ln.slice(2)}</h1>); }
    else if (/^- \[x\] /i.test(ln)) list.push(<li key={i} className="chk done"><Icon name="CircleCheck" className="lucide" />{inline(ln.slice(6))}</li>);
    else if (/^- \[ \] /.test(ln)) list.push(<li key={i} className="chk todo"><Icon name="Circle" className="lucide" />{inline(ln.slice(6))}</li>);
    else if (ln.startsWith("- ")) list.push(<li key={i}>{inline(ln.slice(2))}</li>);
    else if (ln.trim() === "") { flush(); }
    else { flush(); out.push(<p key={i}>{inline(ln)}</p>); }
  });
  flush(); return out;
}

function PlanView({ session, onChange }) {
  const [mode, setMode] = useStateC("split");
  const content = session.planContent || "";
  const readOnly = session.status !== "active";
  return (
    <div className="plan">
      <div className="plan-toolbar">
        <span className="lbl">Plan Document</span>
        <div className="grp">
          <button className={`iconbtn ${mode === "editor" ? "on" : ""}`} onClick={() => setMode("editor")} title="Editor"><Icon name="Pencil" size={14} /></button>
          <button className={`iconbtn ${mode === "split" ? "on" : ""}`} onClick={() => setMode("split")} title="Split"><Icon name="Columns2" size={14} /></button>
          <button className={`iconbtn ${mode === "preview" ? "on" : ""}`} onClick={() => setMode("preview")} title="Preview"><Icon name="Eye" size={14} /></button>
        </div>
      </div>
      <div className="plan-body">
        {(mode === "editor" || mode === "split") && (
          <div className="plan-editor">
            {content || !readOnly
              ? <textarea value={content} readOnly={readOnly} onChange={(e) => onChange(e.target.value)} placeholder="Start writing your plan..." />
              : <div style={{ display: "flex", height: "100%", alignItems: "center", justifyContent: "center", flexDirection: "column", color: "var(--fg-subtle)" }}>
                  <Icon name="FileText" size={32} /><p style={{ color: "var(--fg-muted)", marginBottom: 2 }}>No plan yet</p>
                  <p style={{ fontSize: 12 }}>Start writing to define what this session should accomplish.</p>
                </div>}
          </div>
        )}
        {(mode === "preview" || mode === "split") && (
          <div className="plan-preview">{content ? renderPlan(content) : <p style={{ color: "var(--fg-subtle)", fontStyle: "italic" }}>Nothing to preview yet.</p>}</div>
        )}
      </div>
    </div>
  );
}

/* ── Files ────────────────────────────────────────────────────── */
function FileTreeNode({ node, depth, selected, onSelect }) {
  const [open, setOpen] = useStateC(node.open ?? false);
  if (node.type === "dir") {
    return (
      <div>
        <button className="frow" style={{ paddingLeft: 8 + depth * 14 }} onClick={() => setOpen(!open)}>
          <Icon name={open ? "ChevronDown" : "ChevronRight"} className="lucide" />
          <Icon name={open ? "FolderOpen" : "Folder"} className="lucide" />{node.name}
        </button>
        {open && node.children.map((c, i) => <FileTreeNode key={i} node={c} depth={depth + 1} selected={selected} onSelect={onSelect} />)}
      </div>
    );
  }
  return (
    <button className={`frow ${selected === node.path ? "sel" : ""}`} style={{ paddingLeft: 8 + depth * 14 + 16 }} onClick={() => onSelect(node.path)}>
      <Icon name="File" className="lucide" />{node.name}
      {node.git && <span className={`gs ${node.git}`}>{node.git}</span>}
    </button>
  );
}

function FilesView({ session }) {
  const tree = window.DEUCE.files[session.id];
  const contents = window.DEUCE.fileContents;
  const firstFile = "internal/auth/validate.go";
  const [sel, setSel] = useStateC(tree ? firstFile : null);
  if (!tree) return <div style={{ display: "flex", height: "100%", alignItems: "center", justifyContent: "center", color: "var(--fg-subtle)", fontSize: 13 }}>No files indexed for this session yet.</div>;
  const code = contents[sel];
  const renderSeg = (seg, k) => Array.isArray(seg) ? <span key={k} className={seg[0]}>{seg[1]}</span> : <React.Fragment key={k}>{seg}</React.Fragment>;
  return (
    <div className="files">
      <div className="files-tree">{tree.map((n, i) => <FileTreeNode key={i} node={n} depth={0} selected={sel} onSelect={setSel} />)}</div>
      <div className="files-content">
        <div className="file-head"><Icon name="File" className="lucide" />{sel}</div>
        {code
          ? <div className="file-code">{code.map((row, i) => (
              <div key={i}><span className="ln">{i + 1}</span>{row.length === 0 ? " " : row.map(renderSeg)}</div>
            ))}</div>
          : <div style={{ padding: 16, color: "var(--fg-subtle)", fontSize: 13 }}>Select a file to view its contents.</div>}
      </div>
    </div>
  );
}

/* ── Terminal / Logs ──────────────────────────────────────────── */
function TerminalView() {
  const lines = window.DEUCE.terminalLines;
  const seg = (row) => { const out = []; for (let i = 0; i < row.length; i += 2) out.push(<span key={i} className={row[i]}>{row[i + 1]}</span>); return out; };
  return (
    <div className="term">
      {lines.map((row, i) => <div key={i}>{seg(row)}{i === lines.length - 1 && <span className="cursor" />}</div>)}
    </div>
  );
}

function LogsView() {
  const lines = window.DEUCE.logLines;
  return <div className="logs">{lines.map((l, i) => <div key={i} className={l[0]}>{l[1]}</div>)}</div>;
}

/* ── Center panel shell ───────────────────────────────────────── */
const TABS = [
  { id: "chat", label: "Chat", icon: "MessageSquare" },
  { id: "plan", label: "Plan", icon: "FileText" },
  { id: "files", label: "Files", icon: "FolderTree" },
  { id: "terminal", label: "Terminal", icon: "Terminal" },
];

function CenterPanel({ session, tab, setTab, showLogs, setShowLogs, chatProps, onPlanChange }) {
  if (!session) {
    return <div className="pane-center"><div style={{ flex: 1, display: "flex", alignItems: "center", justifyContent: "center", textAlign: "center" }}>
      <div><h2 style={{ fontSize: 18, color: "var(--fg-emphasis)", margin: 0 }}>Welcome to Deuce</h2>
      <p style={{ fontSize: 14, color: "var(--fg-muted)", marginTop: 8 }}>Select a session from the sidebar or create a new one to get started.</p></div>
    </div></div>;
  }
  const isBuilding = session.workspaceStatus === "starting";
  return (
    <div className="pane-center">
      <div className="cp-head">
        <div className="cp-title">
          <span className="h"># {session.name}</span>
          {session.status === "paused" && <span className="cp-pill paused">Paused</span>}
          {session.status === "archived" && <span className="cp-pill archived">Archived</span>}
        </div>
        {session.description && <span className="cp-desc">{session.description}</span>}
      </div>
      <div className="tabbar">
        {TABS.map((t) => (
          <button key={t.id} className={`tab ${tab === t.id && !showLogs ? "active" : ""}`} onClick={() => { setTab(t.id); setShowLogs(false); }}>
            <Icon name={t.icon} className="lucide" />{t.label}
          </button>
        ))}
        <button className="tab right" title="Open in VS Code"><Icon name="Code" className="lucide" />VS Code</button>
        <button className={`tab ${showLogs ? "active" : ""}`} onClick={() => setShowLogs(!showLogs)} style={{ position: "relative" }}>
          <span style={{ position: "relative", display: "inline-flex" }}>
            <Icon name="ScrollText" className="lucide" />
            {isBuilding && <span className="sdot pulse" style={{ position: "absolute", top: -3, right: -3, width: 7, height: 7, background: "var(--warning)" }} />}
          </span>Logs
        </button>
      </div>
      <div className="cp-body">
        {showLogs ? <LogsView />
          : tab === "chat" ? <ChatView {...chatProps} />
          : tab === "plan" ? <PlanView session={session} onChange={onPlanChange} />
          : tab === "files" ? <FilesView session={session} />
          : <TerminalView />}
      </div>
    </div>
  );
}

Object.assign(window, { CenterPanel });
