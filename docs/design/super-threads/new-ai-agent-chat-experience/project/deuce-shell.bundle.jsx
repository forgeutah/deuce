/* Super Threads — shared Deuce shell atoms + seed content. Exports to window. */

function Icon({ name, size = 16, className = "lucide", style }) {
  const lib = window.LucideIcons;
  const node = lib && lib[name];
  if (!node) return <span className={className} style={{ display: "inline-block", width: size, height: size, ...style }} data-icon={name} />;
  return (
    <svg xmlns="http://www.w3.org/2000/svg" width={size} height={size} viewBox="0 0 24 24"
      fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"
      className={className} style={style}>
      {node.map((child, i) => React.createElement(child[0], { key: i, ...child[1] }))}
    </svg>
  );
}

/* ── Agents (Deuce role colors) ───────────────────────────────── */
const AG = {
  coder:    { id: "coder",    name: "Coder",    color: "#58a6ff", role: "Writes & modifies code", model: "Claude Sonnet 4" },
  reviewer: { id: "reviewer", name: "Reviewer", color: "#BE8FFF", role: "Reviews diffs critically", model: "Claude Sonnet 4" },
  tester:   { id: "tester",   name: "Tester",   color: "#d29922", role: "Writes & runs tests", model: "Claude Sonnet 4" },
};

/* mention renderer */
function mention(text) {
  return text.split(/(@\w+)/g).map((p, i) =>
    /^@\w+$/.test(p) ? <span key={i} style={{ color: "var(--accent)", fontWeight: 500 }}>{p}</span> : <React.Fragment key={i}>{p}</React.Fragment>);
}

/* avatar chip */
function Av({ agent, size = 24 }) {
  return <div className="av" style={{ background: agent.color, width: size, height: size, fontSize: size * 0.42, borderRadius: 5, display: "flex", alignItems: "center", justifyContent: "center", color: "#06121f", fontWeight: 700, flexShrink: 0 }}>{agent.name[0]}</div>;
}
function Uav({ seed, size = 24 }) {
  const R = (typeof window !== "undefined" && window.__resources) || {};
  const src = R["av_" + seed] || `https://api.dicebear.com/9.x/avataaars/svg?seed=${seed}`;
  return <img className="avh" src={src} alt="" style={{ width: size, height: size, borderRadius: "50%", flexShrink: 0 }} />;
}

/* ── Mini sidebar ─────────────────────────────────────────────── */
function MiniSidebar() {
  return (
    <div className="st-side">
      <div className="sb-head" style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
        <div className="sb-brand" style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <img className="mark" src={((typeof window !== "undefined" && window.__resources) || {}).logo || "assets/deuce-logo.png"} alt="" />
          <h1>Deuce</h1>
        </div>
        <Icon name="Plus" size={15} style={{ color: "var(--fg-muted)" }} />
      </div>
      <div style={{ padding: "0 12px 8px" }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6, height: 28, padding: "0 8px", background: "var(--bg-input)", border: "1px solid var(--border-muted)", borderRadius: 6 }}>
          <Icon name="Search" size={13} style={{ color: "var(--fg-subtle)" }} />
          <span style={{ fontSize: 12, color: "var(--fg-subtle)" }}>Search sessions…</span>
        </div>
      </div>
      <div className="divider" />
      <div className="st-sesslist">
        <div className="st-grp"><Icon name="ChevronDown" size={12} />forge-api</div>
        <div className="st-sess active">
          <Icon name="Hash" />
          <div style={{ minWidth: 0 }}>
            <div className="nm">auth-module</div>
            <div className="ds">JWT validation & refresh-token flow</div>
          </div>
          <div className="rt"><span className="sdot" style={{ background: "var(--success)" }} /><span className="unread" style={{ background: "var(--danger)" }}>3</span></div>
        </div>
        <div className="st-sess">
          <Icon name="Hash" />
          <div style={{ minWidth: 0 }}>
            <div className="nm">api-rate-limiting</div>
            <div className="ds">Token-bucket limiter via Redis</div>
          </div>
          <div className="rt"><span className="sdot" style={{ background: "var(--success)" }} /></div>
        </div>
        <div className="st-grp" style={{ marginTop: 6 }}><Icon name="ChevronDown" size={12} />forge-web</div>
        <div className="st-sess">
          <Icon name="Hash" />
          <div style={{ minWidth: 0 }}>
            <div className="nm">homepage-redesign</div>
            <div className="ds">Marketing refresh + hero anim</div>
          </div>
          <div className="rt"><span className="sdot" style={{ background: "var(--success)" }} /><span className="unread" style={{ background: "var(--danger)" }}>1</span></div>
        </div>
      </div>
    </div>
  );
}

/* ── Center chrome (header + tabs) ────────────────────────────── */
function CenterChrome({ children, dim }) {
  return (
    <div className="st-center">
      <div className="st-cphead">
        <div className="st-cptitle"><Icon name="Hash" /><span className="h">auth-module</span></div>
        <span className="st-cpdesc">JWT validation and refresh-token flow for the v2 API</span>
      </div>
      <div className="st-tabbar">
        <button className="st-tab active"><Icon name="MessageSquare" />Chat</button>
        <button className="st-tab"><Icon name="FileText" />Plan</button>
        <button className="st-tab"><Icon name="FolderTree" />Files</button>
        <button className="st-tab"><Icon name="Terminal" />Terminal</button>
      </div>
      {children}
    </div>
  );
}

/* ── Plain channel messages ───────────────────────────────────── */
function HumanMsg({ name, seed, time, children }) {
  return (
    <div className="st-msg">
      <div className="hd"><Uav seed={seed} /><span className="nm">{name}</span><span className="ts">{time}</span></div>
      <div className="bd">{mention(typeof children === "string" ? children : "")}{typeof children !== "string" && children}</div>
    </div>
  );
}
function AgentMsg({ agent, time, children }) {
  return (
    <div className="st-msg" style={{ borderLeft: `2px solid ${agent.color}`, background: `color-mix(in srgb, ${agent.color} 6%, transparent)` }}>
      <div className="hd"><Av agent={agent} /><span className="nm">{agent.name}</span><span className="ts">{time}</span></div>
      <div className="bd">{children}</div>
    </div>
  );
}

/* ── Composer (channel) ───────────────────────────────────────── */
function ChannelComposer({ text = "Message (@ to mention an agent)" }) {
  return (
    <div className="st-composer">
      <div className="row"><span className="ph">{text}</span><button className="send"><Icon name="SendHorizontal" /></button></div>
    </div>
  );
}

/* ── Default right rail (participants + activity) ─────────────── */
function DefaultRail() {
  return (
    <div className="st-rail">
      <div className="sec" style={{ padding: "12px 14px" }}>
        <div className="eyebrow"><Icon name="Users" size={13} />Participants <span style={{ color: "var(--fg-subtle)", fontWeight: 400 }}>(5)</span></div>
        <div className="subhead" style={{ display: "flex", alignItems: "center", gap: 4, margin: "0 0 4px 0", fontSize: 10, color: "var(--fg-subtle)" }}><Icon name="Bot" size={12} />Agents</div>
        {[AG.coder, AG.reviewer, AG.tester].map((a) => (
          <div key={a.id} className="prow"><Av agent={a} size={24} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 6 }}><span className="nm">{a.name}</span><span className="ag-status pulse" style={{ width: 8, height: 8, borderRadius: "50%", background: "var(--success)" }} /></div>
              <span className="ag-desc">{a.role}</span>
            </div>
          </div>
        ))}
      </div>
      <div className="divider" />
      <div className="sec" style={{ padding: "12px 14px" }}>
        <div className="eyebrow">Activity</div>
        <div className="act"><Icon name="CircleCheck" className="lucide green" size={12} /><span className="act-desc">4/4 tests passing</span><span className="act-when">45m</span></div>
        <div className="act"><Icon name="File" size={12} /><span className="act-desc">validate.go<span className="act-add">+12</span><span className="act-del">-3</span></span><span className="act-when">1h</span></div>
        <div className="act"><Icon name="GitCommit" size={12} /><span className="act-desc">a1b2c3d Add token expiration check</span><span className="act-when">1h</span></div>
      </div>
    </div>
  );
}

window.STShell = { Icon, AG, mention, Av, Uav, MiniSidebar, CenterChrome, HumanMsg, AgentMsg, ChannelComposer, DefaultRail };
