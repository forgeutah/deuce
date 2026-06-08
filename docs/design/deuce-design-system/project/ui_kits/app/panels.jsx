/* Sidebar, Summary rail, Create-session dialog. Shared via window. */
const { useState } = React;

function SessionCard({ session, active, onClick }) {
  const statusClass = { ready: "", starting: "pulse", failed: "", suspended: "" }[session.workspaceStatus];
  const dotColor = { ready: "var(--success)", starting: "var(--warning)", failed: "var(--danger)", suspended: "var(--neutral-7)" }[session.workspaceStatus];
  return (
    <button className={`sess ${active ? "active" : ""} ${session.status === "paused" ? "paused" : ""} ${session.status === "archived" ? "archived" : ""}`} onClick={onClick}>
      <Icon name="Hash" className="lucide hash" />
      <div className="sess-mid">
        <div className="sess-top"><span className="sess-name">{session.name}</span></div>
        {session.description && <div className="sess-desc">{session.description}</div>}
      </div>
      <div className="sess-rt">
        <span className={`sdot ${statusClass}`} style={{ background: dotColor }} />
        {session.unreadCount > 0 && <span className="unread">{session.unreadCount}</span>}
      </div>
    </button>
  );
}

function ProjectGroup({ project, sessions, activeId, onSelect }) {
  const [open, setOpen] = useState(true);
  if (sessions.length === 0) return null;
  return (
    <div className="proj">
      <button className="proj-head" onClick={() => setOpen(!open)}>
        <Icon name={open ? "ChevronDown" : "ChevronRight"} className="lucide" />
        {project.name}
      </button>
      {open && (
        <div className="proj-sessions">
          {sessions.slice().sort((a, b) => new Date(b.lastActivityAt) - new Date(a.lastActivityAt)).map((s) => (
            <SessionCard key={s.id} session={s} active={s.id === activeId} onClick={() => onSelect(s.id)} />
          ))}
        </div>
      )}
    </div>
  );
}

function Sidebar({ projects, sessions, activeId, onSelect, search, setSearch, onNew }) {
  const filtered = sessions.filter((s) => s.name.toLowerCase().includes(search.toLowerCase()));
  return (
    <div className="pane-sidebar">
      <div className="sb-head">
        <div className="sb-brand">
          <img className="mark" src="../../assets/deuce-logo.png" alt="" />
          <h1>Deuce</h1>
        </div>
        <button className="iconbtn" title="New Session" onClick={onNew}><Icon name="Plus" /></button>
      </div>
      <div className="sb-search">
        <Icon name="Search" className="lucide" />
        <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Search sessions..." />
      </div>
      <div className="divider" />
      <div className="sb-list">
        {projects.map((p) => (
          <ProjectGroup key={p.id} project={p} sessions={filtered.filter((s) => s.projectId === p.id)} activeId={activeId} onSelect={onSelect} />
        ))}
        {filtered.length === 0 && <p style={{ textAlign: "center", fontSize: 12, color: "var(--fg-subtle)", padding: "16px 8px" }}>{search ? "No sessions match your search" : "No sessions yet"}</p>}
      </div>
      <div className="divider" />
      <div className="sb-foot">
        <button><Icon name="Users" />Teams</button>
        <button><Icon name="Key" />SSH Keys</button>
        <button><Icon name="Settings" />Settings</button>
      </div>
    </div>
  );
}

function AgentRow({ agent }) {
  const sc = { idle: "var(--neutral-8)", working: "var(--success)", "warming-up": "var(--warning)", error: "var(--danger)" }[agent.status] || "var(--neutral-8)";
  return (
    <div className="prow">
      <div className="av" style={{ background: agent.color }}>{agent.name[0]}</div>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <span className="nm">{agent.name}</span>
          <span className={`ag-status ${agent.status === "working" ? "pulse" : ""}`} style={{ background: sc }} />
        </div>
        <span className="ag-desc">{agent.description}</span>
      </div>
    </div>
  );
}

function UserRow({ user }) {
  return (
    <div className="prow">
      <div className="uava">
        <img src={user.avatar} alt={user.name} />
        <span className="pres" style={{ background: user.status === "online" ? "var(--success)" : "var(--neutral-7)" }} />
      </div>
      <span className="nm">{user.name}</span>
    </div>
  );
}

function SummaryPanel({ session, activities }) {
  if (!session) return <div className="pane-summary" />;
  const actIcon = { "file-change": "File", "test-run": "CircleCheck", commit: "GitCommit", "agent-action": "Bot" };
  return (
    <div className="pane-summary">
      <div className="sum-sec">
        <div className="eyebrow"><Icon name="Users" className="lucide" />Participants <span style={{ color: "var(--fg-subtle)", fontWeight: 400 }}>({session.agents.length + session.members.length})</span></div>
        {session.agents.length > 0 && (
          <div style={{ marginBottom: 8 }}>
            <div className="subhead"><Icon name="Bot" className="lucide" />Agents</div>
            {session.agents.map((a) => <AgentRow key={a.id} agent={a} />)}
          </div>
        )}
        {session.members.length > 0 && (
          <div>
            <div className="subhead"><Icon name="Users" className="lucide" />Members</div>
            {session.members.map((u) => <UserRow key={u.id} user={u} />)}
          </div>
        )}
      </div>
      <div className="divider" />
      <div className="sum-sec" style={{ flex: 1, overflow: "auto" }}>
        <div className="eyebrow">Activity</div>
        {activities.length === 0 ? (
          <p style={{ fontSize: 12, color: "var(--fg-subtle)", textAlign: "center", padding: "16px 0" }}>No activity yet</p>
        ) : activities.map((a) => (
          <div className="act" key={a.id}>
            <Icon name={actIcon[a.type] || "Bot"} className={`lucide ${a.type === "test-run" ? "green" : ""}`} />
            <span className="act-desc">{a.description}
              {a.add && <span className="act-add">+{a.add}</span>}
              {a.del && <span className="act-del">-{a.del}</span>}
            </span>
            <span className="act-when">{relTime(a.timestamp)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function CreateSessionDialog({ open, onClose, onCreate, projects }) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [projectId, setProjectId] = useState(projects[0]?.id);
  const [picked, setPicked] = useState(["coder"]);
  if (!open) return null;
  const all = window.DEUCE.AGENTS;
  const toggle = (k) => setPicked((p) => (p.includes(k) ? p.filter((x) => x !== k) : [...p, k]));
  const submit = () => {
    if (!name.trim()) return;
    onCreate({ name: name.trim().replace(/\s+/g, "-").toLowerCase(), description: desc.trim(), projectId, agentKeys: picked });
    setName(""); setDesc(""); setPicked(["coder"]);
  };
  return (
    <div className="overlay" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog">
        <h2>New session</h2>
        <p className="sub">A session is a channel for one piece of work, backed by its own dev container.</p>
        <div className="field"><label>Project</label>
          <select value={projectId} onChange={(e) => setProjectId(e.target.value)}>
            {projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
          </select>
        </div>
        <div className="field"><label>Session name</label>
          <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. payments-webhook" onKeyDown={(e) => e.key === "Enter" && submit()} />
        </div>
        <div className="field"><label>Description</label>
          <input value={desc} onChange={(e) => setDesc(e.target.value)} placeholder="What's this session for?" />
        </div>
        <div className="field"><label>Agents</label>
          <div className="agent-pick">
            {Object.entries(all).map(([k, a]) => {
              const on = picked.includes(k);
              return (
                <button key={k} className={`agent-toggle ${on ? "on" : ""}`} onClick={() => toggle(k)}
                  style={on ? { background: a.color, borderColor: a.color } : {}}>
                  <span className="d" style={{ background: on ? "#fff" : a.color }} />{a.name}
                </button>
              );
            })}
          </div>
        </div>
        <div className="dialog-foot">
          <button className="btn btn-ghost" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" disabled={!name.trim()} onClick={submit}><Icon name="Plus" />Create session</button>
        </div>
      </div>
    </div>
  );
}

Object.assign(window, { Sidebar, SummaryPanel, CreateSessionDialog });
