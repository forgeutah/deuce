/* Deuce UI Kit — App root. Wires state, simulated agent replies, dialog. */
const { useState: useS, useMemo, useCallback } = React;

const CANNED = {
  coder: "On it. I'll wire that up and match the existing patterns — give me a moment to make the change and run the build.",
  reviewer: "Reviewing now. I'll read the diff for correctness, edge cases, and test coverage, then leave inline notes.",
  planner: "Let me break this into ordered units with file paths and test scenarios. I'll flag anything we should defer.",
  tester: "I'll add tests that exercise the real interaction paths and run the suite — back shortly with results.",
  designer: "Looking at it through the existing design system. I'll suggest concrete before/after tweaks grounded in our tokens.",
};

function App() {
  const D = window.DEUCE;
  const [sessions, setSessions] = useS(() => D.sessions.map((s) => ({ ...s })));
  const [messages, setMessages] = useS(() => JSON.parse(JSON.stringify(D.messages)));
  const [activities] = useS(() => D.activities);
  const [activeId, setActiveId] = useS("sess-1");
  const [tabMap, setTabMap] = useS({});
  const [showLogs, setShowLogs] = useS(false);
  const [search, setSearch] = useS("");
  const [createOpen, setCreateOpen] = useS(false);
  const [thinking, setThinking] = useS({}); // sessionId -> [agentId]

  const session = sessions.find((s) => s.id === activeId);
  const tab = tabMap[activeId] || "chat";
  const setTab = (t) => setTabMap((m) => ({ ...m, [activeId]: t }));

  const setActive = (id) => {
    setActiveId(id);
    setShowLogs(false);
    setSessions((ss) => ss.map((s) => (s.id === id ? { ...s, unreadCount: 0 } : s)));
  };

  const participants = useMemo(() => session ? [...session.members, ...session.agents] : [], [session]);
  const sessMsgs = messages[activeId] || [];
  const sessThinking = thinking[activeId] || [];

  const stop = (agentId) => setThinking((t) => ({ ...t, [activeId]: (t[activeId] || []).filter((x) => x !== agentId) }));

  const onSend = useCallback((content) => {
    const sid = activeId;
    const sess = sessions.find((s) => s.id === sid);
    const mentioned = [];
    (content.match(/@(\w+)/g) || []).forEach((m) => {
      const a = sess.agents.find((ag) => ag.name.toLowerCase() === m.slice(1).toLowerCase());
      if (a && !mentioned.includes(a)) mentioned.push(a);
    });
    const msg = { id: "u" + Date.now(), authorId: "current-user", authorType: "human", content, createdAt: new Date().toISOString() };
    setMessages((mm) => ({ ...mm, [sid]: [...(mm[sid] || []), msg] }));

    mentioned.forEach((a, idx) => {
      setTimeout(() => setThinking((t) => ({ ...t, [sid]: [...(t[sid] || []), a.id] })), 250 * (idx + 1));
      const delay = 1100 + idx * 900 + Math.random() * 600;
      setTimeout(() => {
        const roleKey = Object.keys(D.AGENTS).find((k) => D.AGENTS[k].id === a.id);
        setThinking((t) => ({ ...t, [sid]: (t[sid] || []).filter((x) => x !== a.id) }));
        setMessages((mm) => ({ ...mm, [sid]: [...(mm[sid] || []), { id: "a" + Date.now() + a.id, authorId: a.id, authorType: "agent", content: CANNED[roleKey] || "Working on it.", createdAt: new Date().toISOString() }] }));
      }, delay + 250 * (idx + 1));
    });
  }, [activeId, sessions]);

  const onPlanChange = (val) => setSessions((ss) => ss.map((s) => (s.id === activeId ? { ...s, planContent: val } : s)));

  const onCreate = ({ name, description, projectId, agentKeys }) => {
    const id = "sess-" + Date.now();
    const newSess = {
      id, name, description, projectId, status: "active", workspaceStatus: "starting", unreadCount: 0,
      agents: agentKeys.map((k) => ({ ...D.AGENTS[k] })), members: [D.USERS.clint],
      lastActivityAt: new Date().toISOString(), planContent: "",
    };
    setSessions((ss) => [...ss, newSess]);
    setMessages((mm) => ({ ...mm, [id]: [] }));
    setCreateOpen(false);
    setActiveId(id);
    setShowLogs(false);
    setTimeout(() => setSessions((ss) => ss.map((s) => (s.id === id ? { ...s, workspaceStatus: "ready" } : s))), 3200);
  };

  return (
    <div className="shell">
      <Sidebar projects={D.projects} sessions={sessions} activeId={activeId} onSelect={setActive}
        search={search} setSearch={setSearch} onNew={() => setCreateOpen(true)} />
      <CenterPanel session={session} tab={tab} setTab={setTab} showLogs={showLogs} setShowLogs={setShowLogs}
        onPlanChange={onPlanChange}
        chatProps={{ session, messages: sessMsgs, participants, thinking: sessThinking, onSend, onStop: stop }} />
      <SummaryPanel session={session} activities={(activities[activeId] || [])} />
      <CreateSessionDialog open={createOpen} onClose={() => setCreateOpen(false)} onCreate={onCreate} projects={D.projects} />
    </div>
  );
}

function boot() {
  ReactDOM.createRoot(document.getElementById("root")).render(<App />);
}
if (window.LucideIcons) boot();
else window.addEventListener("lucide-ready", boot, { once: true });
