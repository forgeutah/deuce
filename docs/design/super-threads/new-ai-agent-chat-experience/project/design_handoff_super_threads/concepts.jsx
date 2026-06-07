/* Super Threads — concept components. Uses window.STShell atoms. */
const { Icon: I2, AG: A, mention: M, Av: AV, Uav: UAV, MiniSidebar: SB, CenterChrome: CC, HumanMsg: HM, AgentMsg: AM, ChannelComposer: COMP, DefaultRail: RAIL } = window.STShell;

/* validate.go diff used throughout */
const DIFF = [
  ["ctx", "@@ -42,8 +42,19 @@ func Validate(token string) error"],
  ["ctx", "   }"],
  ["add", "+  claims, err := ParseClaims(token)"],
  ["add", "+  if err != nil {"],
  ["add", "+    return fmt.Errorf(\"parse claims: %w\", err)"],
  ["add", "+  }"],
  ["add", "+  if claims.ExpiresAt.Before(time.Now()) {"],
  ["add", "+    return ErrTokenExpired"],
  ["add", "+  }"],
  ["ctx", "   return nil"],
];
const Pre = ({ lines }) => <pre>{lines.map((l, i) => <div key={i} className={l[0] === "add" ? "diff-add" : "diff-ctx"}>{l[1]}</div>)}</pre>;

/* ── COLLAPSED CARD — the star, three treatments ─────────────── */
function TypingDots({ color }) {
  return <div style={{ display: "flex", gap: 4 }}>
    {[0, .2, .4].map((d, i) => <span key={i} className="tdot" style={{ background: color, animationDelay: `${d}s` }} />)}
  </div>;
}

/* Variant A — full peek stack + live status + working */
function CardA({ agent, peeks, live, working, unread }) {
  return (
    <div className="st-card" style={{ "--ac": agent.color }}>
      <div className="st-card-hd">
        <AV agent={agent} size={22} />
        <span className="nm">{agent.name}</span>
        <span className="st-thread-pill"><I2 name="GitBranch" />Thread</span>
        <span className="spacer" />
        {unread ? <span className="st-unread">{unread}</span> : null}
        <span className="chev"><I2 name="ChevronRight" /></span>
      </div>
      <div className="st-peek">
        {peeks.map((p, i) => (
          <div key={i} className={`st-peekmsg ${i === 0 ? "dim" : i === 1 ? "dim2" : ""}`}>
            <span className={`who ${p.t}`} style={p.t === "agent" ? { color: agent.color } : {}}>{p.who}</span>
            <span className="st-peek-txt">{p.text}</span>
          </div>
        ))}
      </div>
      {working ? (
        <>
          <div className="st-live"><span className="pip" /><span className="tool">{live.tool}</span><span className="path">{live.path}</span></div>
          <div className="st-typing"><TypingDots color={agent.color} /><span className="lbl">{agent.name} is working…</span></div>
        </>
      ) : (
        <div className="st-live" style={{ borderTopColor: `color-mix(in srgb, ${agent.color} 14%, transparent)` }}>
          <I2 name="CircleCheck" size={12} style={{ color: "var(--success)" }} /><span style={{ color: "var(--fg-muted)" }}>{live.done}</span>
        </div>
      )}
    </div>
  );
}

/* Variant B — mono status + activity sparkline */
function CardB({ agent, last, live, sparks }) {
  return (
    <div className="st-card" style={{ "--ac": agent.color }}>
      <div className="st-card-hd">
        <AV agent={agent} size={22} />
        <span className="nm">{agent.name}</span>
        <span className="role">· {agent.role}</span>
        <span className="spacer" />
        <span className="st-thread-pill"><I2 name="Radio" />Live</span>
      </div>
      <div className="st-spark">{sparks.map((h, i) => <i key={i} style={{ height: h }} />)}</div>
      <div className="st-live"><span className="pip" /><span className="tool">{live.tool}</span><span className="path">{live.path}</span></div>
      <div className="st-peek"><div className="st-peekmsg"><span className="who agent" style={{ color: agent.color }}>{agent.name}</span><span className="st-peek-txt">{last}</span></div></div>
    </div>
  );
}

/* Variant C — minimal bar */
function CardC({ agent, last, working, unread }) {
  return (
    <div className="st-bar" style={{ "--ac": agent.color }}>
      <AV agent={agent} size={20} />
      <span className="nm">{agent.name}</span>
      <span className="last">{last}</span>
      <span className="tail">
        {working ? <TypingDots color={agent.color} /> : <I2 name="CircleCheck" size={13} style={{ color: "var(--success)" }} />}
        {unread ? <span className="st-unread">{unread}</span> : null}
        <I2 name="ChevronRight" size={15} style={{ color: "var(--fg-subtle)" }} />
      </span>
    </div>
  );
}

/* content presets */
const CODER_PEEKS = [
  { t: "human", who: "Clint", text: "@Coder add token expiration checking to Validate" },
  { t: "agent", who: "Coder", text: "On it — parsing claims, comparing ExpiresAt to now." },
];
const CODER_LIVE = { tool: "Edit", path: "internal/auth/validate.go", done: "Build passing · validate.go +12 −3" };
const REVIEWER_PEEKS = [
  { t: "human", who: "Sarah", text: "@Reviewer can you review all the auth changes?" },
  { t: "agent", who: "Reviewer", text: "Reading the diff for edge cases and coverage…" },
];
const REVIEWER_LIVE = { tool: "Read", path: "internal/auth/middleware.go" };

/* ── Channel backdrop: human msgs + collapsed cards for others ── */
function ChannelBackdrop({ collapsedAgents }) {
  return (
    <div className="st-channel">
      <HM name="Clint Berry" seed="Clint" time="2:04 PM">Let's start on the auth module — JWT validation with expiration checking.</HM>
      <AM agent={A.coder} time="2:30 PM">I've set up the auth middleware and user model. Base structure is ready for JWT integration.</AM>
      {collapsedAgents.map((c) => c)}
      <div style={{ flex: 1 }} />
    </div>
  );
}

/* ── Shared thread inner pieces ───────────────────────────────── */
function ThreadHeader({ agent, sub, onClose, top }) {
  return (
    <div className="st-thread-hd" style={{ "--ac": agent.color, ...(top === false ? { borderTop: "none" } : {}) }}>
      <AV agent={agent} size={26} />
      <div style={{ minWidth: 0 }}>
        <div className="nm">{agent.name}</div>
        <div className="sub">{sub}</div>
      </div>
      <button className="x"><I2 name="X" /></button>
    </div>
  );
}
function ThreadMessages({ agent, withWork }) {
  return (
    <div className="st-thread-body">
      <div className="st-thread-msg"><div className="hd"><UAV seed="Clint" /><span className="nm">Clint Berry</span><span className="ts">3:58 PM</span></div>
        <div className="bd">{M("@Coder now add token expiration checking to the Validate function")}</div></div>
      <div className="st-thread-msg fromagent" style={{ "--ac": agent.color }}><div className="hd"><AV agent={agent} size={24} /><span className="nm">{agent.name}</span><span className="ts">3:58 PM</span></div>
        <div className="bd">On it — parsing the JWT claims and comparing <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-emphasis)" }}>ExpiresAt</code> against <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-emphasis)" }}>time.Now()</code>. Updating <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-emphasis)" }}>Validate()</code> now.</div></div>
      {withWork && (
        <div className="st-work">
          <div className="st-work-hd"><I2 name="Pencil" />​<span className="t">internal/auth/validate.go</span><span className="stat">+12 −3</span></div>
          <Pre lines={DIFF} />
        </div>
      )}
      <div className="st-thread-msg fromagent" style={{ "--ac": agent.color }}><div className="hd"><AV agent={agent} size={24} /><span className="nm">{agent.name}</span><span className="ts">4:01 PM</span></div>
        <div className="bd">Done. <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-emphasis)" }}>Validate()</code> now rejects expired tokens with <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-emphasis)" }}>ErrTokenExpired</code>. Build passes.</div></div>
      <div className="st-thread-msg" style={{ paddingTop: 2 }}>
        <div className="st-statusline" style={{ "--ac": agent.color, paddingLeft: 32 }}><span className="pip" /><span style={{ color: "var(--fg-muted)", fontFamily: "var(--font-mono)", fontSize: 12 }}><span style={{ color: agent.color, fontWeight: 600 }}>Run</span> go test ./internal/auth/…</span></div>
      </div>
    </div>
  );
}
function ThreadComposer({ agent }) {
  return (
    <div className="st-thread-composer" style={{ "--ac": agent.color }}>
      <div className="row"><span className="ph">Reply to {agent.name}…</span><button className="send"><I2 name="SendHorizontal" /></button></div>
    </div>
  );
}

/* Right-rail WORK DOCK — live status + diffs/results */
function WorkDock({ agent }) {
  return (
    <div className="st-rail">
      <div className="st-dock">
        <div className="st-dock-hd" style={{ "--ac": agent.color }}>
          <AV agent={agent} size={24} />
          <div><div className="nm">{agent.name}</div><div className="sub" style={{ color: agent.color }}>Live work</div></div>
        </div>
        <div className="st-dock-body" style={{ "--ac": agent.color }}>
          <div>
            <div className="st-dock-eyebrow"><I2 name="Activity" />Doing now</div>
            <div className="st-statusline"><span className="pip" /><span><span className="tool">go test</span> ./internal/auth/…</span></div>
            <div className="st-toolrow done"><I2 name="CircleCheck" />Edited validate.go</div>
            <div className="st-toolrow done"><I2 name="CircleCheck" />Parsed JWT claims</div>
            <div className="st-toolrow run"><I2 name="Loader" />Running test suite</div>
          </div>
          <div>
            <div className="st-dock-eyebrow"><I2 name="FileDiff" />Changes</div>
            <div className="st-diffcard">
              <div className="top"><I2 name="File" />validate.go<span className="add">+12 −3</span></div>
              <Pre lines={DIFF.slice(0, 7)} />
            </div>
          </div>
          <div>
            <div className="st-dock-eyebrow"><I2 name="CircleCheck" />Test results</div>
            <div className="st-diffcard"><div className="top"><I2 name="CircleCheck" style={{ color: "var(--success)" }} />4/4 passing<span className="add">ok 0.003s</span></div></div>
          </div>
        </div>
      </div>
    </div>
  );
}

/* collapsed others used inside channels */
const reviewerCardA = <CardA key="rev" agent={A.reviewer} peeks={REVIEWER_PEEKS} live={REVIEWER_LIVE} working unread={2} />;
const testerCardC = <CardC key="tst" agent={A.tester} last="Tests written and passing — 4/4 green." working={false} unread={0} />;

/* ════════════════ FOCUSED LAYOUT CONCEPTS ════════════════════ */

/* 1 · DRAWER — thread + work combined, slides over the rail */
function DrawerConcept() {
  return (
    <div className="st-app has-drawer">
      <SB />
      <CC><ChannelBackdrop collapsedAgents={[reviewerCardA, testerCardC]} /><COMP /></CC>
      <div className="st-drawer" style={{ "--ac": A.coder.color }}>
        <ThreadHeader agent={A.coder} sub="Super thread · live" />
        <ThreadMessages agent={A.coder} withWork />
        <ThreadComposer agent={A.coder} />
      </div>
    </div>
  );
}

/* 2 · SPLIT — channel | thread, work dock in far rail */
function SplitConcept() {
  return (
    <div className="st-app">
      <SB />
      <div className="st-center">
        <div className="st-cphead"><div className="st-cptitle"><I2 name="Hash" /><span className="h">auth-module</span></div><span className="st-cpdesc">JWT validation and refresh-token flow for the v2 API</span></div>
        <div className="st-tabbar"><button className="st-tab active"><I2 name="MessageSquare" />Chat</button><button className="st-tab"><I2 name="FileText" />Plan</button><button className="st-tab"><I2 name="FolderTree" />Files</button></div>
        <div className="st-split">
          <div className="chan"><ChannelBackdrop collapsedAgents={[<CardC key="r" agent={A.reviewer} last="Reading the diff for edge cases…" working unread={2} />, testerCardC]} /></div>
          <div className="thr" style={{ "--ac": A.coder.color }}>
            <ThreadHeader agent={A.coder} sub="Super thread" />
            <ThreadMessages agent={A.coder} withWork={false} />
            <ThreadComposer agent={A.coder} />
          </div>
        </div>
      </div>
      <WorkDock agent={A.coder} />
    </div>
  );
}

/* 3 · OVERLAY — float over dimmed channel */
function OverlayConcept() {
  return (
    <div className="st-app">
      <SB />
      <CC><ChannelBackdrop collapsedAgents={[reviewerCardA, testerCardC]} /><COMP /></CC>
      <RAIL />
      <div className="st-dim" />
      <div className="st-overlay" style={{ "--ac": A.coder.color }}>
        <ThreadHeader agent={A.coder} sub="Super thread · live" top={false} />
        <ThreadMessages agent={A.coder} withWork />
        <ThreadComposer agent={A.coder} />
      </div>
    </div>
  );
}

/* 4 · TAKEOVER — thread fills center, work dock in rail */
function TakeoverConcept() {
  return (
    <div className="st-app">
      <SB />
      <div className="st-center">
        <div className="st-breadcrumb">
          <button className="back"><I2 name="ChevronLeft" />auth-module</button>
          <span className="crumb-x"><I2 name="ChevronRight" size={13} /></span>
          <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}><span style={{ width: 8, height: 8, borderRadius: "50%", background: A.coder.color, display: "inline-block" }} /><span className="cur">Coder thread</span></span>
        </div>
        <div className="st-thread" style={{ "--ac": A.coder.color, flex: 1 }}>
          <ThreadHeader agent={A.coder} sub="Writes & modifies code · Claude Sonnet 4" />
          <ThreadMessages agent={A.coder} withWork={false} />
          <ThreadComposer agent={A.coder} />
        </div>
      </div>
      <WorkDock agent={A.coder} />
    </div>
  );
}

/* ════════════════ NOVEL · TUNE-IN STATION BOARD ═════════════ */
function Station({ agent, freq, now, tuned, idle, heights }) {
  return (
    <div className={`st-station ${tuned ? "tuned" : ""} ${idle ? "idle" : ""}`} style={{ "--ac": agent.color }}>
      <div className="st-station-hd"><AV agent={agent} size={26} /><span className="nm">{agent.name}</span><span className="freq"><I2 name={idle ? "RadioReceiver" : "Radio"} />{freq}</span></div>
      <div className="st-wave">{heights.map((h, i) => <i key={i} style={{ height: idle ? 3 : h, animationDelay: `${(i % 7) * 0.09}s` }} />)}</div>
      <div className="st-station-foot">{idle
        ? <><I2 name="CircleCheck" size={13} style={{ color: "var(--success)" }} /><span>{now}</span></>
        : <><span className="now">{now}</span></>}
        <span style={{ marginLeft: "auto", display: "inline-flex", alignItems: "center", gap: 5, color: tuned ? agent.color : "var(--fg-subtle)", fontSize: 12, fontWeight: 600 }}>{tuned ? "Tuned in" : "Tune in"}<I2 name="ChevronRight" size={14} /></span>
      </div>
    </div>
  );
}
const W = (n, base) => Array.from({ length: n }, (_, i) => base[i % base.length]);
function StationConcept() {
  return (
    <div className="st-app">
      <SB />
      <div className="st-center">
        <div className="st-cphead"><div className="st-cptitle"><I2 name="Hash" /><span className="h">auth-module</span></div><span className="st-cpdesc">3 agents broadcasting · tune into one to step inside its thread</span></div>
        <div className="st-tabbar"><button className="st-tab active"><I2 name="Radio" />Stations</button><button className="st-tab"><I2 name="MessageSquare" />Channel</button><button className="st-tab"><I2 name="FileText" />Plan</button></div>
        <div className="st-stations">
          <Station agent={A.coder} freq="98.5 working" now="Edit · validate.go" tuned heights={W(40, [18, 26, 12, 30, 8, 22, 16, 28])} />
          <Station agent={A.reviewer} freq="101.3 working" now="Read · middleware.go" heights={W(40, [10, 20, 14, 24, 9, 18])} />
          <Station agent={A.tester} freq="—— idle" now="4/4 tests passing · 45m ago" idle heights={W(40, [4])} />
        </div>
        <COMP text="Broadcast to the room (@ to tune an agent)" />
      </div>
      <WorkDock agent={A.coder} />
    </div>
  );
}

window.STConcepts = { CardA, CardB, CardC, CODER_PEEKS, CODER_LIVE, REVIEWER_PEEKS, REVIEWER_LIVE,
  DrawerConcept, SplitConcept, OverlayConcept, TakeoverConcept, StationConcept };
