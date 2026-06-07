/* @ds-bundle: {"format":3,"namespace":"DeuceDesignSystem_021b5d","components":[],"sourceHashes":{"ui_kits/app/app.jsx":"72d9d9916283","ui_kits/app/center.jsx":"9e6d83576695","ui_kits/app/data.js":"37f6a22cc2dc","ui_kits/app/icon.jsx":"c4559c2d8031","ui_kits/app/panels.jsx":"d601e73aa624"},"inlinedExternals":[],"unexposedExports":[]} */

(() => {

const __ds_ns = (window.DeuceDesignSystem_021b5d = window.DeuceDesignSystem_021b5d || {});

const __ds_scope = {};

(__ds_ns.__errors = __ds_ns.__errors || []);

// ui_kits/app/app.jsx
try { (() => {
/* Deuce UI Kit — App root. Wires state, simulated agent replies, dialog. */
const {
  useState: useS,
  useMemo,
  useCallback
} = React;
const CANNED = {
  coder: "On it. I'll wire that up and match the existing patterns — give me a moment to make the change and run the build.",
  reviewer: "Reviewing now. I'll read the diff for correctness, edge cases, and test coverage, then leave inline notes.",
  planner: "Let me break this into ordered units with file paths and test scenarios. I'll flag anything we should defer.",
  tester: "I'll add tests that exercise the real interaction paths and run the suite — back shortly with results.",
  designer: "Looking at it through the existing design system. I'll suggest concrete before/after tweaks grounded in our tokens."
};
function App() {
  const D = window.DEUCE;
  const [sessions, setSessions] = useS(() => D.sessions.map(s => ({
    ...s
  })));
  const [messages, setMessages] = useS(() => JSON.parse(JSON.stringify(D.messages)));
  const [activities] = useS(() => D.activities);
  const [activeId, setActiveId] = useS("sess-1");
  const [tabMap, setTabMap] = useS({});
  const [showLogs, setShowLogs] = useS(false);
  const [search, setSearch] = useS("");
  const [createOpen, setCreateOpen] = useS(false);
  const [thinking, setThinking] = useS({}); // sessionId -> [agentId]

  const session = sessions.find(s => s.id === activeId);
  const tab = tabMap[activeId] || "chat";
  const setTab = t => setTabMap(m => ({
    ...m,
    [activeId]: t
  }));
  const setActive = id => {
    setActiveId(id);
    setShowLogs(false);
    setSessions(ss => ss.map(s => s.id === id ? {
      ...s,
      unreadCount: 0
    } : s));
  };
  const participants = useMemo(() => session ? [...session.members, ...session.agents] : [], [session]);
  const sessMsgs = messages[activeId] || [];
  const sessThinking = thinking[activeId] || [];
  const stop = agentId => setThinking(t => ({
    ...t,
    [activeId]: (t[activeId] || []).filter(x => x !== agentId)
  }));
  const onSend = useCallback(content => {
    const sid = activeId;
    const sess = sessions.find(s => s.id === sid);
    const mentioned = [];
    (content.match(/@(\w+)/g) || []).forEach(m => {
      const a = sess.agents.find(ag => ag.name.toLowerCase() === m.slice(1).toLowerCase());
      if (a && !mentioned.includes(a)) mentioned.push(a);
    });
    const msg = {
      id: "u" + Date.now(),
      authorId: "current-user",
      authorType: "human",
      content,
      createdAt: new Date().toISOString()
    };
    setMessages(mm => ({
      ...mm,
      [sid]: [...(mm[sid] || []), msg]
    }));
    mentioned.forEach((a, idx) => {
      setTimeout(() => setThinking(t => ({
        ...t,
        [sid]: [...(t[sid] || []), a.id]
      })), 250 * (idx + 1));
      const delay = 1100 + idx * 900 + Math.random() * 600;
      setTimeout(() => {
        const roleKey = Object.keys(D.AGENTS).find(k => D.AGENTS[k].id === a.id);
        setThinking(t => ({
          ...t,
          [sid]: (t[sid] || []).filter(x => x !== a.id)
        }));
        setMessages(mm => ({
          ...mm,
          [sid]: [...(mm[sid] || []), {
            id: "a" + Date.now() + a.id,
            authorId: a.id,
            authorType: "agent",
            content: CANNED[roleKey] || "Working on it.",
            createdAt: new Date().toISOString()
          }]
        }));
      }, delay + 250 * (idx + 1));
    });
  }, [activeId, sessions]);
  const onPlanChange = val => setSessions(ss => ss.map(s => s.id === activeId ? {
    ...s,
    planContent: val
  } : s));
  const onCreate = ({
    name,
    description,
    projectId,
    agentKeys
  }) => {
    const id = "sess-" + Date.now();
    const newSess = {
      id,
      name,
      description,
      projectId,
      status: "active",
      workspaceStatus: "starting",
      unreadCount: 0,
      agents: agentKeys.map(k => ({
        ...D.AGENTS[k]
      })),
      members: [D.USERS.clint],
      lastActivityAt: new Date().toISOString(),
      planContent: ""
    };
    setSessions(ss => [...ss, newSess]);
    setMessages(mm => ({
      ...mm,
      [id]: []
    }));
    setCreateOpen(false);
    setActiveId(id);
    setShowLogs(false);
    setTimeout(() => setSessions(ss => ss.map(s => s.id === id ? {
      ...s,
      workspaceStatus: "ready"
    } : s)), 3200);
  };
  return /*#__PURE__*/React.createElement("div", {
    className: "shell"
  }, /*#__PURE__*/React.createElement(Sidebar, {
    projects: D.projects,
    sessions: sessions,
    activeId: activeId,
    onSelect: setActive,
    search: search,
    setSearch: setSearch,
    onNew: () => setCreateOpen(true)
  }), /*#__PURE__*/React.createElement(CenterPanel, {
    session: session,
    tab: tab,
    setTab: setTab,
    showLogs: showLogs,
    setShowLogs: setShowLogs,
    onPlanChange: onPlanChange,
    chatProps: {
      session,
      messages: sessMsgs,
      participants,
      thinking: sessThinking,
      onSend,
      onStop: stop
    }
  }), /*#__PURE__*/React.createElement(SummaryPanel, {
    session: session,
    activities: activities[activeId] || []
  }), /*#__PURE__*/React.createElement(CreateSessionDialog, {
    open: createOpen,
    onClose: () => setCreateOpen(false),
    onCreate: onCreate,
    projects: D.projects
  }));
}
function boot() {
  ReactDOM.createRoot(document.getElementById("root")).render(/*#__PURE__*/React.createElement(App, null));
}
if (window.LucideIcons) boot();else window.addEventListener("lucide-ready", boot, {
  once: true
});
})(); } catch (e) { __ds_ns.__errors.push({ path: "ui_kits/app/app.jsx", error: String((e && e.message) || e) }); }

// ui_kits/app/center.jsx
try { (() => {
/* CenterPanel + Chat / Plan / Files / Terminal / Logs views. Shared via window. */
const {
  useState: useStateC,
  useRef: useRefC,
  useEffect: useEffectC
} = React;

/* ── Chat ─────────────────────────────────────────────────────── */
function Expandable({
  item
}) {
  const [open, setOpen] = useStateC(false);
  return /*#__PURE__*/React.createElement("div", {
    className: "expand"
  }, /*#__PURE__*/React.createElement("button", {
    className: "expand-btn",
    onClick: () => setOpen(!open)
  }, open ? "Hide" : "Show", " ", item.title), open && /*#__PURE__*/React.createElement("div", {
    className: "expand-box"
  }, /*#__PURE__*/React.createElement("div", {
    className: "expand-sum"
  }, item.summary), /*#__PURE__*/React.createElement("pre", null, item.lines.map((l, i) => /*#__PURE__*/React.createElement("div", {
    key: i,
    className: l[0] === "add" ? "diff-add" : l[0] === "ctx" ? "diff-ctx" : ""
  }, l[1])))));
}
function Message({
  msg,
  author,
  showHeader
}) {
  const isAgent = msg.authorType === "agent";
  const style = isAgent && author ? {
    borderLeftColor: author.color,
    background: `${author.color}08`
  } : undefined;
  return /*#__PURE__*/React.createElement("div", {
    className: `msg ${isAgent ? "agent" : ""}`,
    style: style
  }, showHeader && /*#__PURE__*/React.createElement("div", {
    className: "msg-hd"
  }, isAgent && author ? /*#__PURE__*/React.createElement("div", {
    className: "av",
    style: {
      background: author.color
    }
  }, author.name[0]) : /*#__PURE__*/React.createElement("img", {
    className: "avh",
    src: author?.avatar,
    alt: ""
  }), /*#__PURE__*/React.createElement("span", {
    className: "msg-name"
  }, author?.name), /*#__PURE__*/React.createElement("span", {
    className: "msg-ts"
  }, clockTime(msg.createdAt))), /*#__PURE__*/React.createElement("div", {
    className: "msg-body",
    style: !showHeader ? {
      paddingLeft: 36
    } : undefined
  }, renderContent(msg.content), msg.expandable?.map((it, i) => /*#__PURE__*/React.createElement(Expandable, {
    key: i,
    item: it
  }))));
}
function TypingIndicator({
  agent,
  onStop
}) {
  return /*#__PURE__*/React.createElement("div", {
    className: "typing"
  }, /*#__PURE__*/React.createElement("div", {
    className: "typing-row"
  }, /*#__PURE__*/React.createElement("div", {
    className: "av",
    style: {
      background: agent.color
    }
  }, agent.name[0]), /*#__PURE__*/React.createElement("span", {
    className: "typing-name"
  }, agent.name, " is working"), /*#__PURE__*/React.createElement("div", {
    style: {
      display: "flex",
      gap: 4
    }
  }, /*#__PURE__*/React.createElement("span", {
    className: "tdot",
    style: {
      animationDelay: "0s"
    }
  }), /*#__PURE__*/React.createElement("span", {
    className: "tdot",
    style: {
      animationDelay: ".2s"
    }
  }), /*#__PURE__*/React.createElement("span", {
    className: "tdot",
    style: {
      animationDelay: ".4s"
    }
  })), /*#__PURE__*/React.createElement("button", {
    className: "stop",
    title: "Stop agent",
    onClick: onStop
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Square",
    size: 12
  }))));
}
function ChatView({
  session,
  messages,
  participants,
  thinking,
  onSend,
  onStop
}) {
  const [input, setInput] = useStateC("");
  const scrollRef = useRefC(null);
  useEffectC(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length, thinking.length, session.id]);
  const findAuthor = m => m.authorType === "agent" ? session.agents.find(a => a.id === m.authorId) : participants.find(p => p.id === m.authorId);
  const readOnly = session.status !== "active";
  const send = () => {
    if (input.trim() && !readOnly) {
      onSend(input.trim());
      setInput("");
    }
  };
  return /*#__PURE__*/React.createElement("div", {
    className: "chat"
  }, /*#__PURE__*/React.createElement("div", {
    className: "chat-scroll",
    ref: scrollRef
  }, messages.length === 0 && /*#__PURE__*/React.createElement("div", {
    className: "chat-empty"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Bot",
    className: "lucide"
  }), /*#__PURE__*/React.createElement("h3", null, "Start a conversation"), /*#__PURE__*/React.createElement("p", null, "@mention an agent to get started.", /*#__PURE__*/React.createElement("span", {
    style: {
      display: "block",
      marginTop: 8
    }
  }, session.agents.map(a => /*#__PURE__*/React.createElement("button", {
    key: a.id,
    className: "mention-chip",
    style: {
      background: a.color
    },
    onClick: () => setInput(`@${a.name} `)
  }, "@", a.name))))), messages.map((m, i) => {
    const prev = messages[i - 1];
    const showHeader = !prev || prev.authorId !== m.authorId || new Date(m.createdAt) - new Date(prev.createdAt) > 300000;
    return /*#__PURE__*/React.createElement(Message, {
      key: m.id,
      msg: m,
      author: findAuthor(m),
      showHeader: showHeader
    });
  }), thinking.map(id => {
    const a = session.agents.find(x => x.id === id);
    return a ? /*#__PURE__*/React.createElement(TypingIndicator, {
      key: id,
      agent: a,
      onStop: () => onStop(id)
    }) : null;
  }), /*#__PURE__*/React.createElement("div", null)), /*#__PURE__*/React.createElement("div", {
    className: "composer"
  }, readOnly ? /*#__PURE__*/React.createElement("div", {
    className: "readonly"
  }, session.status === "paused" ? "Session is paused" : "Session is archived") : /*#__PURE__*/React.createElement("div", {
    className: "composer-row"
  }, /*#__PURE__*/React.createElement("textarea", {
    value: input,
    rows: 1,
    placeholder: "Message (@ to mention an agent)",
    onChange: e => setInput(e.target.value),
    onKeyDown: e => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        send();
      }
    }
  }), /*#__PURE__*/React.createElement("button", {
    className: "send",
    disabled: !input.trim(),
    onClick: send
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "SendHorizontal"
  })))));
}

/* ── Plan ─────────────────────────────────────────────────────── */
function renderPlan(content) {
  const out = [];
  const lines = content.split("\n");
  let list = [];
  const flush = () => {
    if (list.length) {
      out.push(/*#__PURE__*/React.createElement("ul", {
        key: "u" + out.length
      }, list));
      list = [];
    }
  };
  const inline = t => t.split(/(`[^`]+`)/g).map((p, i) => p.startsWith("`") ? /*#__PURE__*/React.createElement("code", {
    key: i
  }, p.slice(1, -1)) : /*#__PURE__*/React.createElement(React.Fragment, {
    key: i
  }, p));
  lines.forEach((ln, i) => {
    if (ln.startsWith("## ")) {
      flush();
      out.push(/*#__PURE__*/React.createElement("h2", {
        key: i
      }, ln.slice(3)));
    } else if (ln.startsWith("# ")) {
      flush();
      out.push(/*#__PURE__*/React.createElement("h1", {
        key: i
      }, ln.slice(2)));
    } else if (/^- \[x\] /i.test(ln)) list.push(/*#__PURE__*/React.createElement("li", {
      key: i,
      className: "chk done"
    }, /*#__PURE__*/React.createElement(Icon, {
      name: "CircleCheck",
      className: "lucide"
    }), inline(ln.slice(6))));else if (/^- \[ \] /.test(ln)) list.push(/*#__PURE__*/React.createElement("li", {
      key: i,
      className: "chk todo"
    }, /*#__PURE__*/React.createElement(Icon, {
      name: "Circle",
      className: "lucide"
    }), inline(ln.slice(6))));else if (ln.startsWith("- ")) list.push(/*#__PURE__*/React.createElement("li", {
      key: i
    }, inline(ln.slice(2))));else if (ln.trim() === "") {
      flush();
    } else {
      flush();
      out.push(/*#__PURE__*/React.createElement("p", {
        key: i
      }, inline(ln)));
    }
  });
  flush();
  return out;
}
function PlanView({
  session,
  onChange
}) {
  const [mode, setMode] = useStateC("split");
  const content = session.planContent || "";
  const readOnly = session.status !== "active";
  return /*#__PURE__*/React.createElement("div", {
    className: "plan"
  }, /*#__PURE__*/React.createElement("div", {
    className: "plan-toolbar"
  }, /*#__PURE__*/React.createElement("span", {
    className: "lbl"
  }, "Plan Document"), /*#__PURE__*/React.createElement("div", {
    className: "grp"
  }, /*#__PURE__*/React.createElement("button", {
    className: `iconbtn ${mode === "editor" ? "on" : ""}`,
    onClick: () => setMode("editor"),
    title: "Editor"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Pencil",
    size: 14
  })), /*#__PURE__*/React.createElement("button", {
    className: `iconbtn ${mode === "split" ? "on" : ""}`,
    onClick: () => setMode("split"),
    title: "Split"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Columns2",
    size: 14
  })), /*#__PURE__*/React.createElement("button", {
    className: `iconbtn ${mode === "preview" ? "on" : ""}`,
    onClick: () => setMode("preview"),
    title: "Preview"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Eye",
    size: 14
  })))), /*#__PURE__*/React.createElement("div", {
    className: "plan-body"
  }, (mode === "editor" || mode === "split") && /*#__PURE__*/React.createElement("div", {
    className: "plan-editor"
  }, content || !readOnly ? /*#__PURE__*/React.createElement("textarea", {
    value: content,
    readOnly: readOnly,
    onChange: e => onChange(e.target.value),
    placeholder: "Start writing your plan..."
  }) : /*#__PURE__*/React.createElement("div", {
    style: {
      display: "flex",
      height: "100%",
      alignItems: "center",
      justifyContent: "center",
      flexDirection: "column",
      color: "var(--fg-subtle)"
    }
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "FileText",
    size: 32
  }), /*#__PURE__*/React.createElement("p", {
    style: {
      color: "var(--fg-muted)",
      marginBottom: 2
    }
  }, "No plan yet"), /*#__PURE__*/React.createElement("p", {
    style: {
      fontSize: 12
    }
  }, "Start writing to define what this session should accomplish."))), (mode === "preview" || mode === "split") && /*#__PURE__*/React.createElement("div", {
    className: "plan-preview"
  }, content ? renderPlan(content) : /*#__PURE__*/React.createElement("p", {
    style: {
      color: "var(--fg-subtle)",
      fontStyle: "italic"
    }
  }, "Nothing to preview yet."))));
}

/* ── Files ────────────────────────────────────────────────────── */
function FileTreeNode({
  node,
  depth,
  selected,
  onSelect
}) {
  const [open, setOpen] = useStateC(node.open ?? false);
  if (node.type === "dir") {
    return /*#__PURE__*/React.createElement("div", null, /*#__PURE__*/React.createElement("button", {
      className: "frow",
      style: {
        paddingLeft: 8 + depth * 14
      },
      onClick: () => setOpen(!open)
    }, /*#__PURE__*/React.createElement(Icon, {
      name: open ? "ChevronDown" : "ChevronRight",
      className: "lucide"
    }), /*#__PURE__*/React.createElement(Icon, {
      name: open ? "FolderOpen" : "Folder",
      className: "lucide"
    }), node.name), open && node.children.map((c, i) => /*#__PURE__*/React.createElement(FileTreeNode, {
      key: i,
      node: c,
      depth: depth + 1,
      selected: selected,
      onSelect: onSelect
    })));
  }
  return /*#__PURE__*/React.createElement("button", {
    className: `frow ${selected === node.path ? "sel" : ""}`,
    style: {
      paddingLeft: 8 + depth * 14 + 16
    },
    onClick: () => onSelect(node.path)
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "File",
    className: "lucide"
  }), node.name, node.git && /*#__PURE__*/React.createElement("span", {
    className: `gs ${node.git}`
  }, node.git));
}
function FilesView({
  session
}) {
  const tree = window.DEUCE.files[session.id];
  const contents = window.DEUCE.fileContents;
  const firstFile = "internal/auth/validate.go";
  const [sel, setSel] = useStateC(tree ? firstFile : null);
  if (!tree) return /*#__PURE__*/React.createElement("div", {
    style: {
      display: "flex",
      height: "100%",
      alignItems: "center",
      justifyContent: "center",
      color: "var(--fg-subtle)",
      fontSize: 13
    }
  }, "No files indexed for this session yet.");
  const code = contents[sel];
  const renderSeg = (seg, k) => Array.isArray(seg) ? /*#__PURE__*/React.createElement("span", {
    key: k,
    className: seg[0]
  }, seg[1]) : /*#__PURE__*/React.createElement(React.Fragment, {
    key: k
  }, seg);
  return /*#__PURE__*/React.createElement("div", {
    className: "files"
  }, /*#__PURE__*/React.createElement("div", {
    className: "files-tree"
  }, tree.map((n, i) => /*#__PURE__*/React.createElement(FileTreeNode, {
    key: i,
    node: n,
    depth: 0,
    selected: sel,
    onSelect: setSel
  }))), /*#__PURE__*/React.createElement("div", {
    className: "files-content"
  }, /*#__PURE__*/React.createElement("div", {
    className: "file-head"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "File",
    className: "lucide"
  }), sel), code ? /*#__PURE__*/React.createElement("div", {
    className: "file-code"
  }, code.map((row, i) => /*#__PURE__*/React.createElement("div", {
    key: i
  }, /*#__PURE__*/React.createElement("span", {
    className: "ln"
  }, i + 1), row.length === 0 ? " " : row.map(renderSeg)))) : /*#__PURE__*/React.createElement("div", {
    style: {
      padding: 16,
      color: "var(--fg-subtle)",
      fontSize: 13
    }
  }, "Select a file to view its contents.")));
}

/* ── Terminal / Logs ──────────────────────────────────────────── */
function TerminalView() {
  const lines = window.DEUCE.terminalLines;
  const seg = row => {
    const out = [];
    for (let i = 0; i < row.length; i += 2) out.push(/*#__PURE__*/React.createElement("span", {
      key: i,
      className: row[i]
    }, row[i + 1]));
    return out;
  };
  return /*#__PURE__*/React.createElement("div", {
    className: "term"
  }, lines.map((row, i) => /*#__PURE__*/React.createElement("div", {
    key: i
  }, seg(row), i === lines.length - 1 && /*#__PURE__*/React.createElement("span", {
    className: "cursor"
  }))));
}
function LogsView() {
  const lines = window.DEUCE.logLines;
  return /*#__PURE__*/React.createElement("div", {
    className: "logs"
  }, lines.map((l, i) => /*#__PURE__*/React.createElement("div", {
    key: i,
    className: l[0]
  }, l[1])));
}

/* ── Center panel shell ───────────────────────────────────────── */
const TABS = [{
  id: "chat",
  label: "Chat",
  icon: "MessageSquare"
}, {
  id: "plan",
  label: "Plan",
  icon: "FileText"
}, {
  id: "files",
  label: "Files",
  icon: "FolderTree"
}, {
  id: "terminal",
  label: "Terminal",
  icon: "Terminal"
}];
function CenterPanel({
  session,
  tab,
  setTab,
  showLogs,
  setShowLogs,
  chatProps,
  onPlanChange
}) {
  if (!session) {
    return /*#__PURE__*/React.createElement("div", {
      className: "pane-center"
    }, /*#__PURE__*/React.createElement("div", {
      style: {
        flex: 1,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        textAlign: "center"
      }
    }, /*#__PURE__*/React.createElement("div", null, /*#__PURE__*/React.createElement("h2", {
      style: {
        fontSize: 18,
        color: "var(--fg-emphasis)",
        margin: 0
      }
    }, "Welcome to Deuce"), /*#__PURE__*/React.createElement("p", {
      style: {
        fontSize: 14,
        color: "var(--fg-muted)",
        marginTop: 8
      }
    }, "Select a session from the sidebar or create a new one to get started."))));
  }
  const isBuilding = session.workspaceStatus === "starting";
  return /*#__PURE__*/React.createElement("div", {
    className: "pane-center"
  }, /*#__PURE__*/React.createElement("div", {
    className: "cp-head"
  }, /*#__PURE__*/React.createElement("div", {
    className: "cp-title"
  }, /*#__PURE__*/React.createElement("span", {
    className: "h"
  }, "# ", session.name), session.status === "paused" && /*#__PURE__*/React.createElement("span", {
    className: "cp-pill paused"
  }, "Paused"), session.status === "archived" && /*#__PURE__*/React.createElement("span", {
    className: "cp-pill archived"
  }, "Archived")), session.description && /*#__PURE__*/React.createElement("span", {
    className: "cp-desc"
  }, session.description)), /*#__PURE__*/React.createElement("div", {
    className: "tabbar"
  }, TABS.map(t => /*#__PURE__*/React.createElement("button", {
    key: t.id,
    className: `tab ${tab === t.id && !showLogs ? "active" : ""}`,
    onClick: () => {
      setTab(t.id);
      setShowLogs(false);
    }
  }, /*#__PURE__*/React.createElement(Icon, {
    name: t.icon,
    className: "lucide"
  }), t.label)), /*#__PURE__*/React.createElement("button", {
    className: "tab right",
    title: "Open in VS Code"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Code",
    className: "lucide"
  }), "VS Code"), /*#__PURE__*/React.createElement("button", {
    className: `tab ${showLogs ? "active" : ""}`,
    onClick: () => setShowLogs(!showLogs),
    style: {
      position: "relative"
    }
  }, /*#__PURE__*/React.createElement("span", {
    style: {
      position: "relative",
      display: "inline-flex"
    }
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "ScrollText",
    className: "lucide"
  }), isBuilding && /*#__PURE__*/React.createElement("span", {
    className: "sdot pulse",
    style: {
      position: "absolute",
      top: -3,
      right: -3,
      width: 7,
      height: 7,
      background: "var(--warning)"
    }
  })), "Logs")), /*#__PURE__*/React.createElement("div", {
    className: "cp-body"
  }, showLogs ? /*#__PURE__*/React.createElement(LogsView, null) : tab === "chat" ? /*#__PURE__*/React.createElement(ChatView, chatProps) : tab === "plan" ? /*#__PURE__*/React.createElement(PlanView, {
    session: session,
    onChange: onPlanChange
  }) : tab === "files" ? /*#__PURE__*/React.createElement(FilesView, {
    session: session
  }) : /*#__PURE__*/React.createElement(TerminalView, null)));
}
Object.assign(window, {
  CenterPanel
});
})(); } catch (e) { __ds_ns.__errors.push({ path: "ui_kits/app/center.jsx", error: String((e && e.message) || e) }); }

// ui_kits/app/data.js
try { (() => {
/* Deuce UI Kit — seed data (mirrors src/mocks/data/seed.ts). Plain globals. */
(function () {
  const now = Date.now();
  const mins = m => new Date(now - m * 60000).toISOString();
  const hrs = h => new Date(now - h * 3600000).toISOString();
  const days = d => new Date(now - d * 86400000).toISOString();
  const AGENTS = {
    coder: {
      id: "agent-coder",
      name: "Coder",
      color: "#58a6ff",
      status: "idle",
      provider: "Anthropic",
      model: "Claude Sonnet 4",
      description: "Writes and modifies code"
    },
    reviewer: {
      id: "agent-reviewer",
      name: "Reviewer",
      color: "#BE8FFF",
      status: "idle",
      provider: "Anthropic",
      model: "Claude Sonnet 4",
      description: "Reviews code changes"
    },
    planner: {
      id: "agent-planner",
      name: "Planner",
      color: "#3fb950",
      status: "idle",
      provider: "OpenAI",
      model: "GPT-4o",
      description: "Creates implementation plans"
    },
    tester: {
      id: "agent-tester",
      name: "Tester",
      color: "#d29922",
      status: "idle",
      provider: "Anthropic",
      model: "Claude Sonnet 4",
      description: "Writes and runs tests"
    },
    designer: {
      id: "agent-designer",
      name: "Designer",
      color: "#f778ba",
      status: "idle",
      provider: "OpenAI",
      model: "GPT-4o",
      description: "UI/UX suggestions"
    }
  };
  const agent = k => ({
    ...AGENTS[k]
  });
  const USERS = {
    clint: {
      id: "current-user",
      name: "Clint Berry",
      email: "clint@forge.dev",
      avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Clint",
      status: "online"
    },
    sarah: {
      id: "user-2",
      name: "Sarah Chen",
      email: "sarah@forge.dev",
      avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Sarah",
      status: "online"
    },
    mike: {
      id: "user-3",
      name: "Mike Rodriguez",
      email: "mike@forge.dev",
      avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Mike",
      status: "offline"
    },
    alex: {
      id: "user-4",
      name: "Alex Park",
      email: "alex@acme.co",
      avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Alex",
      status: "online"
    },
    jordan: {
      id: "user-5",
      name: "Jordan Lee",
      email: "jordan@acme.co",
      avatar: "https://api.dicebear.com/9.x/avataaars/svg?seed=Jordan",
      status: "offline"
    }
  };
  const projects = [{
    id: "proj-1",
    name: "forge-api",
    teamId: "team-1"
  }, {
    id: "proj-2",
    name: "forge-web",
    teamId: "team-1"
  }, {
    id: "proj-3",
    name: "acme-dashboard",
    teamId: "team-2"
  }];
  const sessions = [{
    id: "sess-1",
    name: "auth-module",
    description: "JWT validation and refresh-token flow for the v2 API",
    projectId: "proj-1",
    status: "active",
    workspaceStatus: "ready",
    unreadCount: 3,
    agents: [agent("coder"), agent("reviewer"), agent("tester")],
    members: [USERS.clint, USERS.sarah],
    lastActivityAt: mins(5),
    planContent: `# Auth Module Plan

## Goals
- [ ] Implement JWT token validation
- [ ] Add token expiration checks
- [x] Set up auth middleware
- [x] Create user model

## Technical Notes
- Using \`golang-jwt/jwt/v5\` for JWT parsing
- Token expiry window: 24 hours
- Refresh tokens stored in Redis

## Acceptance Criteria
- All endpoints behind auth middleware return 401 without valid token
- Expired tokens are rejected with appropriate error message
- Token refresh flow works end-to-end`
  }, {
    id: "sess-2",
    name: "api-rate-limiting",
    description: "Token-bucket rate limiter via Redis, per-endpoint config",
    projectId: "proj-1",
    status: "active",
    workspaceStatus: "ready",
    unreadCount: 0,
    agents: [agent("coder"), agent("planner")],
    members: [USERS.clint, USERS.mike],
    lastActivityAt: hrs(2),
    planContent: `# Rate Limiting Plan

## Approach
- Token bucket algorithm
- Per-user rate limits via Redis
- Configurable limits per endpoint

## TODO
- [ ] Implement rate limiter middleware
- [ ] Add Redis integration
- [ ] Configure per-route limits`
  }, {
    id: "sess-3",
    name: "homepage-redesign",
    description: "Marketing homepage refresh with the new hero animation",
    projectId: "proj-2",
    status: "active",
    workspaceStatus: "ready",
    unreadCount: 1,
    agents: [agent("coder"), agent("designer")],
    members: [USERS.clint, USERS.sarah],
    lastActivityAt: mins(30),
    planContent: ""
  }, {
    id: "sess-4",
    name: "ci-pipeline",
    description: "",
    projectId: "proj-1",
    status: "paused",
    workspaceStatus: "suspended",
    unreadCount: 0,
    agents: [agent("coder")],
    members: [USERS.mike],
    lastActivityAt: days(2),
    planContent: ""
  }, {
    id: "sess-6",
    name: "dashboard-charts",
    description: "Recharts integration for the customer analytics dashboard",
    projectId: "proj-3",
    status: "active",
    workspaceStatus: "starting",
    unreadCount: 2,
    agents: [agent("coder"), agent("reviewer"), agent("tester")],
    members: [USERS.alex, USERS.jordan],
    lastActivityAt: mins(15),
    planContent: ""
  }];
  const messages = {
    "sess-1": [{
      id: "m1",
      authorId: "current-user",
      authorType: "human",
      content: "Let's start working on the auth module. We need JWT validation with token expiration checking.",
      createdAt: hrs(4)
    }, {
      id: "m2",
      authorId: "user-2",
      authorType: "human",
      content: "Agreed. I've updated the plan with acceptance criteria. @Planner can you break this down?",
      createdAt: hrs(3.5)
    }, {
      id: "m3",
      authorId: "agent-coder",
      authorType: "agent",
      content: "I've set up the auth middleware and user model. The base structure is ready for JWT integration.",
      createdAt: hrs(3),
      expandable: [{
        title: "changes",
        summary: "internal/auth/middleware.go (+45 -0)",
        lines: [["add", "+func Middleware(next http.Handler) http.Handler {"], ["add", "+  return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {"], ["add", "+    token := extractToken(r)"], ["add", "+    if token == \"\" {"], ["add", "+      http.Error(w, \"unauthorized\", http.StatusUnauthorized)"], ["add", "+      return"], ["add", "+    }"], ["add", "+    next.ServeHTTP(w, r)"], ["add", "+  })"], ["add", "+}"]]
      }]
    }, {
      id: "m4",
      authorId: "current-user",
      authorType: "human",
      content: "@Coder now add token expiration checking to the Validate function",
      createdAt: hrs(2)
    }, {
      id: "m5",
      authorId: "agent-coder",
      authorType: "agent",
      content: "I've updated the Validate function with token expiration checking. The implementation parses JWT claims and checks the expiry timestamp.",
      createdAt: hrs(1.5),
      expandable: [{
        title: "changes",
        summary: "internal/auth/validate.go (+12 -3)",
        lines: [["ctx", "@@ -42,8 +42,19 @@ func Validate(token string) error"], ["ctx", "   }"], ["add", "+  claims, err := ParseClaims(token)"], ["add", "+  if err != nil {"], ["add", "+    return fmt.Errorf(\"parse claims: %w\", err)"], ["add", "+  }"], ["add", "+  if claims.ExpiresAt.Before(time.Now()) {"], ["add", "+    return ErrTokenExpired"], ["add", "+  }"], ["ctx", "   return nil"], ["ctx", " }"]]
      }]
    }, {
      id: "m6",
      authorId: "current-user",
      authorType: "human",
      content: "@Tester can you write tests for the new expiration logic?",
      createdAt: hrs(1)
    }, {
      id: "m7",
      authorId: "agent-tester",
      authorType: "agent",
      content: "Tests are written and passing. I've covered valid tokens, expired tokens, invalid format, and empty input.",
      createdAt: mins(45),
      expandable: [{
        title: "test results",
        summary: "4/4 passing",
        lines: [["ctx", "=== RUN   TestValidate"], ["add", "--- PASS: TestValidate (0.00s)"], ["ctx", "=== RUN   TestValidateExpired"], ["add", "--- PASS: TestValidateExpired (0.00s)"], ["ctx", "=== RUN   TestValidateInvalid"], ["add", "--- PASS: TestValidateInvalid (0.00s)"], ["ctx", "=== RUN   TestValidateEmpty"], ["add", "--- PASS: TestValidateEmpty (0.00s)"], ["add", "PASS"], ["ctx", "ok  forge-api/internal/auth  0.003s"]]
      }]
    }, {
      id: "m8",
      authorId: "user-2",
      authorType: "human",
      content: "@Reviewer can you review all the auth changes?",
      createdAt: mins(20)
    }, {
      id: "m9",
      authorId: "agent-reviewer",
      authorType: "agent",
      content: "Code looks good overall. Two suggestions:\n\n1. Consider using `errors.Is()` for error comparison in middleware\n2. Add context to the JWT parsing errors for better debugging\n\nThe token expiration logic is correctly implemented. Tests cover the main cases well.",
      createdAt: mins(10)
    }],
    "sess-2": [{
      id: "m20",
      authorId: "current-user",
      authorType: "human",
      content: "We need rate limiting on the API endpoints. @Planner can you outline an approach?",
      createdAt: hrs(6)
    }, {
      id: "m21",
      authorId: "agent-planner",
      authorType: "agent",
      content: "Here's my recommended approach for rate limiting:\n\n**Algorithm:** Token bucket (allows bursts while maintaining average rate)\n\n**Storage:** Redis for distributed rate limit state\n\n**Configuration:**\n- Default: 100 requests/minute per user\n- Auth endpoints: 10 requests/minute (stricter)\n- Public endpoints: 30 requests/minute per IP\n\n**Implementation phases:**\n1. Core rate limiter with in-memory store (for dev)\n2. Redis adapter\n3. Per-route configuration\n4. Rate limit headers in responses",
      createdAt: hrs(5.5)
    }, {
      id: "m22",
      authorId: "user-3",
      authorType: "human",
      content: "Looks good. Make sure we add X-RateLimit headers in the response.",
      createdAt: hrs(5)
    }],
    "sess-3": [{
      id: "m30",
      authorId: "current-user",
      authorType: "human",
      content: "We need to redesign the homepage. @Designer any ideas for improving the layout?",
      createdAt: hrs(8)
    }, {
      id: "m31",
      authorId: "agent-designer",
      authorType: "agent",
      content: "Here are my recommendations for the homepage redesign:\n\n1. **Hero section:** Move the CTA above the fold with a clear value proposition\n2. **Social proof:** Add a customer logos bar below the hero\n3. **Features grid:** Replace the feature list with a 3-column card layout\n4. **Dark/light contrast:** Use alternating section backgrounds for visual rhythm\n\nThe current layout has too much text density. Let's prioritize visual hierarchy.",
      createdAt: hrs(7)
    }],
    "sess-4": [],
    "sess-6": [{
      id: "m60",
      authorId: "user-4",
      authorType: "human",
      content: "Starting work on the dashboard charts. We need bar charts, line charts, and a pie chart for the overview.",
      createdAt: hrs(3)
    }, {
      id: "m61",
      authorId: "user-5",
      authorType: "human",
      content: "@Coder can you set up Recharts and create a basic bar chart component?",
      createdAt: hrs(2.5)
    }]
  };
  const activities = {
    "sess-1": [{
      id: "a1",
      type: "agent-action",
      description: "Reviewer completed code review",
      timestamp: mins(10)
    }, {
      id: "a2",
      type: "test-run",
      description: "4/4 tests passing",
      timestamp: mins(45)
    }, {
      id: "a3",
      type: "file-change",
      description: "validate.go",
      timestamp: hrs(1.5),
      add: "12",
      del: "3"
    }, {
      id: "a4",
      type: "file-change",
      description: "middleware.go",
      timestamp: hrs(3),
      add: "45",
      del: "0"
    }, {
      id: "a5",
      type: "commit",
      description: "a1b2c3d Add token expiration check",
      timestamp: hrs(1)
    }],
    "sess-2": [{
      id: "a20",
      type: "agent-action",
      description: "Planner created implementation plan",
      timestamp: hrs(5.5)
    }],
    "sess-3": [],
    "sess-4": [],
    "sess-6": []
  };
  const files = {
    "sess-1": [{
      name: "internal",
      type: "dir",
      open: true,
      children: [{
        name: "auth",
        type: "dir",
        open: true,
        children: [{
          name: "middleware.go",
          type: "file",
          git: "M",
          path: "internal/auth/middleware.go"
        }, {
          name: "validate.go",
          type: "file",
          git: "M",
          path: "internal/auth/validate.go"
        }, {
          name: "validate_test.go",
          type: "file",
          git: "A",
          path: "internal/auth/validate_test.go"
        }]
      }, {
        name: "api",
        type: "dir",
        open: false,
        children: [{
          name: "router.go",
          type: "file",
          path: "internal/api/router.go"
        }]
      }]
    }, {
      name: "cmd/server/main.go",
      type: "file",
      path: "cmd/server/main.go"
    }, {
      name: "go.mod",
      type: "file",
      path: "go.mod"
    }, {
      name: "README.md",
      type: "file",
      path: "README.md"
    }]
  };
  const fileContents = {
    "internal/auth/validate.go": [["kw", "package", " auth"], [], ["kw", "import", " ("], ["str", "  \"errors\""], ["str", "  \"fmt\""], ["str", "  \"time\""], [")"], [], ["kw", "var", " ("], ["  ErrInvalidFormat = ", ["fn", "errors.New"], ["str", "(\"invalid token format\")"]], ["  ErrTokenExpired  = ", ["fn", "errors.New"], ["str", "(\"token expired\")"]], [")"], [], ["kw", "func ", ["fn", "Validate"], "(token ", ["ty", "string"], ") ", ["ty", "error"], " {"], ["  ", ["kw", "if"], " token == ", ["str", "\"\""], " {"], ["    ", ["kw", "return"], " ErrInvalidFormat"], ["  }"], [], ["  ", ["cm", "// Check token expiration"]], ["  claims, err := ", ["fn", "ParseClaims"], "(token)"], ["  ", ["kw", "if"], " err != ", ["kw", "nil"], " {"], ["    ", ["kw", "return fmt"], ".", ["fn", "Errorf"], "(", ["str", "\"parse claims: %w\""], ", err)"], ["  }"], [], ["  ", ["kw", "if"], " claims.ExpiresAt.", ["fn", "Before"], "(time.", ["fn", "Now"], "()) {"], ["    ", ["kw", "return"], " ErrTokenExpired"], ["  }"], [], ["  ", ["kw", "return nil"]], ["}"]]
  };
  const terminalLines = [["prompt", "vscode ➜ ", "path", "/workspaces/forge-api", " (auth-module) $ ", "go test ./internal/auth/..."], ["out", "ok  \tforge-api/internal/auth\t0.004s"], ["prompt", "vscode ➜ ", "path", "/workspaces/forge-api", " (auth-module) $ ", "git status -s"], ["out", " M internal/auth/middleware.go"], ["out", " M internal/auth/validate.go"], ["out", "?? internal/auth/validate_test.go"], ["prompt", "vscode ➜ ", "path", "/workspaces/forge-api", " (auth-module) $ "]];
  const logLines = [["info", "[devpod] creating workspace 'auth-module'..."], ["out", "[devpod] provider: docker"], ["out", "[devpod] pulling image mcr.microsoft.com/devcontainers/go:1.22"], ["ok", "[devpod] image ready"], ["out", "[devpod] starting container..."], ["ok", "[devpod] container started (id 9f3ac2)"], ["out", "[devpod] running postCreateCommand: go mod download"], ["ok", "[devpod] workspace ready in 18.2s"], ["info", "[agent] Coder connected via devpod ssh"]];
  window.DEUCE = {
    AGENTS,
    USERS,
    projects,
    sessions,
    messages,
    activities,
    files,
    fileContents,
    terminalLines,
    logLines
  };
})();
})(); } catch (e) { __ds_ns.__errors.push({ path: "ui_kits/app/data.js", error: String((e && e.message) || e) }); }

// ui_kits/app/icon.jsx
try { (() => {
/* Icon + small helpers. Shared via window. */

function Icon({
  name,
  size = 16,
  className = "lucide",
  style
}) {
  const lib = window.LucideIcons;
  const node = lib && lib[name];
  if (!node) return /*#__PURE__*/React.createElement("span", {
    className: className,
    style: {
      display: "inline-block",
      width: size,
      height: size,
      ...style
    },
    "data-icon": name
  });
  return /*#__PURE__*/React.createElement("svg", {
    xmlns: "http://www.w3.org/2000/svg",
    width: size,
    height: size,
    viewBox: "0 0 24 24",
    fill: "none",
    stroke: "currentColor",
    strokeWidth: "2",
    strokeLinecap: "round",
    strokeLinejoin: "round",
    className: className,
    style: style
  }, node.map((child, i) => React.createElement(child[0], {
    key: i,
    ...child[1]
  })));
}
function relTime(ts) {
  const diff = Date.now() - new Date(ts).getTime();
  const m = Math.floor(diff / 60000);
  if (m < 1) return "just now";
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}
function clockTime(ts) {
  return new Date(ts).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit"
  });
}

// Render @mentions inside message text with an accent color.
function renderContent(text) {
  const parts = text.split(/(@\w+)/g);
  return parts.map((p, i) => /^@\w+$/.test(p) ? /*#__PURE__*/React.createElement("span", {
    className: "mention",
    key: i
  }, p) : /*#__PURE__*/React.createElement(React.Fragment, {
    key: i
  }, p));
}
Object.assign(window, {
  Icon,
  relTime,
  clockTime,
  renderContent
});
})(); } catch (e) { __ds_ns.__errors.push({ path: "ui_kits/app/icon.jsx", error: String((e && e.message) || e) }); }

// ui_kits/app/panels.jsx
try { (() => {
/* Sidebar, Summary rail, Create-session dialog. Shared via window. */
const {
  useState
} = React;
function SessionCard({
  session,
  active,
  onClick
}) {
  const statusClass = {
    ready: "",
    starting: "pulse",
    failed: "",
    suspended: ""
  }[session.workspaceStatus];
  const dotColor = {
    ready: "var(--success)",
    starting: "var(--warning)",
    failed: "var(--danger)",
    suspended: "var(--neutral-7)"
  }[session.workspaceStatus];
  return /*#__PURE__*/React.createElement("button", {
    className: `sess ${active ? "active" : ""} ${session.status === "paused" ? "paused" : ""} ${session.status === "archived" ? "archived" : ""}`,
    onClick: onClick
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Hash",
    className: "lucide hash"
  }), /*#__PURE__*/React.createElement("div", {
    className: "sess-mid"
  }, /*#__PURE__*/React.createElement("div", {
    className: "sess-top"
  }, /*#__PURE__*/React.createElement("span", {
    className: "sess-name"
  }, session.name)), session.description && /*#__PURE__*/React.createElement("div", {
    className: "sess-desc"
  }, session.description)), /*#__PURE__*/React.createElement("div", {
    className: "sess-rt"
  }, /*#__PURE__*/React.createElement("span", {
    className: `sdot ${statusClass}`,
    style: {
      background: dotColor
    }
  }), session.unreadCount > 0 && /*#__PURE__*/React.createElement("span", {
    className: "unread"
  }, session.unreadCount)));
}
function ProjectGroup({
  project,
  sessions,
  activeId,
  onSelect
}) {
  const [open, setOpen] = useState(true);
  if (sessions.length === 0) return null;
  return /*#__PURE__*/React.createElement("div", {
    className: "proj"
  }, /*#__PURE__*/React.createElement("button", {
    className: "proj-head",
    onClick: () => setOpen(!open)
  }, /*#__PURE__*/React.createElement(Icon, {
    name: open ? "ChevronDown" : "ChevronRight",
    className: "lucide"
  }), project.name), open && /*#__PURE__*/React.createElement("div", {
    className: "proj-sessions"
  }, sessions.slice().sort((a, b) => new Date(b.lastActivityAt) - new Date(a.lastActivityAt)).map(s => /*#__PURE__*/React.createElement(SessionCard, {
    key: s.id,
    session: s,
    active: s.id === activeId,
    onClick: () => onSelect(s.id)
  }))));
}
function Sidebar({
  projects,
  sessions,
  activeId,
  onSelect,
  search,
  setSearch,
  onNew
}) {
  const filtered = sessions.filter(s => s.name.toLowerCase().includes(search.toLowerCase()));
  return /*#__PURE__*/React.createElement("div", {
    className: "pane-sidebar"
  }, /*#__PURE__*/React.createElement("div", {
    className: "sb-head"
  }, /*#__PURE__*/React.createElement("div", {
    className: "sb-brand"
  }, /*#__PURE__*/React.createElement("img", {
    className: "mark",
    src: "../../assets/deuce-logo.png",
    alt: ""
  }), /*#__PURE__*/React.createElement("h1", null, "Deuce")), /*#__PURE__*/React.createElement("button", {
    className: "iconbtn",
    title: "New Session",
    onClick: onNew
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Plus"
  }))), /*#__PURE__*/React.createElement("div", {
    className: "sb-search"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Search",
    className: "lucide"
  }), /*#__PURE__*/React.createElement("input", {
    value: search,
    onChange: e => setSearch(e.target.value),
    placeholder: "Search sessions..."
  })), /*#__PURE__*/React.createElement("div", {
    className: "divider"
  }), /*#__PURE__*/React.createElement("div", {
    className: "sb-list"
  }, projects.map(p => /*#__PURE__*/React.createElement(ProjectGroup, {
    key: p.id,
    project: p,
    sessions: filtered.filter(s => s.projectId === p.id),
    activeId: activeId,
    onSelect: onSelect
  })), filtered.length === 0 && /*#__PURE__*/React.createElement("p", {
    style: {
      textAlign: "center",
      fontSize: 12,
      color: "var(--fg-subtle)",
      padding: "16px 8px"
    }
  }, search ? "No sessions match your search" : "No sessions yet")), /*#__PURE__*/React.createElement("div", {
    className: "divider"
  }), /*#__PURE__*/React.createElement("div", {
    className: "sb-foot"
  }, /*#__PURE__*/React.createElement("button", null, /*#__PURE__*/React.createElement(Icon, {
    name: "Users"
  }), "Teams"), /*#__PURE__*/React.createElement("button", null, /*#__PURE__*/React.createElement(Icon, {
    name: "Key"
  }), "SSH Keys"), /*#__PURE__*/React.createElement("button", null, /*#__PURE__*/React.createElement(Icon, {
    name: "Settings"
  }), "Settings")));
}
function AgentRow({
  agent
}) {
  const sc = {
    idle: "var(--neutral-8)",
    working: "var(--success)",
    "warming-up": "var(--warning)",
    error: "var(--danger)"
  }[agent.status] || "var(--neutral-8)";
  return /*#__PURE__*/React.createElement("div", {
    className: "prow"
  }, /*#__PURE__*/React.createElement("div", {
    className: "av",
    style: {
      background: agent.color
    }
  }, agent.name[0]), /*#__PURE__*/React.createElement("div", {
    style: {
      flex: 1,
      minWidth: 0
    }
  }, /*#__PURE__*/React.createElement("div", {
    style: {
      display: "flex",
      alignItems: "center",
      gap: 6
    }
  }, /*#__PURE__*/React.createElement("span", {
    className: "nm"
  }, agent.name), /*#__PURE__*/React.createElement("span", {
    className: `ag-status ${agent.status === "working" ? "pulse" : ""}`,
    style: {
      background: sc
    }
  })), /*#__PURE__*/React.createElement("span", {
    className: "ag-desc"
  }, agent.description)));
}
function UserRow({
  user
}) {
  return /*#__PURE__*/React.createElement("div", {
    className: "prow"
  }, /*#__PURE__*/React.createElement("div", {
    className: "uava"
  }, /*#__PURE__*/React.createElement("img", {
    src: user.avatar,
    alt: user.name
  }), /*#__PURE__*/React.createElement("span", {
    className: "pres",
    style: {
      background: user.status === "online" ? "var(--success)" : "var(--neutral-7)"
    }
  })), /*#__PURE__*/React.createElement("span", {
    className: "nm"
  }, user.name));
}
function SummaryPanel({
  session,
  activities
}) {
  if (!session) return /*#__PURE__*/React.createElement("div", {
    className: "pane-summary"
  });
  const actIcon = {
    "file-change": "File",
    "test-run": "CircleCheck",
    commit: "GitCommit",
    "agent-action": "Bot"
  };
  return /*#__PURE__*/React.createElement("div", {
    className: "pane-summary"
  }, /*#__PURE__*/React.createElement("div", {
    className: "sum-sec"
  }, /*#__PURE__*/React.createElement("div", {
    className: "eyebrow"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Users",
    className: "lucide"
  }), "Participants ", /*#__PURE__*/React.createElement("span", {
    style: {
      color: "var(--fg-subtle)",
      fontWeight: 400
    }
  }, "(", session.agents.length + session.members.length, ")")), session.agents.length > 0 && /*#__PURE__*/React.createElement("div", {
    style: {
      marginBottom: 8
    }
  }, /*#__PURE__*/React.createElement("div", {
    className: "subhead"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Bot",
    className: "lucide"
  }), "Agents"), session.agents.map(a => /*#__PURE__*/React.createElement(AgentRow, {
    key: a.id,
    agent: a
  }))), session.members.length > 0 && /*#__PURE__*/React.createElement("div", null, /*#__PURE__*/React.createElement("div", {
    className: "subhead"
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Users",
    className: "lucide"
  }), "Members"), session.members.map(u => /*#__PURE__*/React.createElement(UserRow, {
    key: u.id,
    user: u
  })))), /*#__PURE__*/React.createElement("div", {
    className: "divider"
  }), /*#__PURE__*/React.createElement("div", {
    className: "sum-sec",
    style: {
      flex: 1,
      overflow: "auto"
    }
  }, /*#__PURE__*/React.createElement("div", {
    className: "eyebrow"
  }, "Activity"), activities.length === 0 ? /*#__PURE__*/React.createElement("p", {
    style: {
      fontSize: 12,
      color: "var(--fg-subtle)",
      textAlign: "center",
      padding: "16px 0"
    }
  }, "No activity yet") : activities.map(a => /*#__PURE__*/React.createElement("div", {
    className: "act",
    key: a.id
  }, /*#__PURE__*/React.createElement(Icon, {
    name: actIcon[a.type] || "Bot",
    className: `lucide ${a.type === "test-run" ? "green" : ""}`
  }), /*#__PURE__*/React.createElement("span", {
    className: "act-desc"
  }, a.description, a.add && /*#__PURE__*/React.createElement("span", {
    className: "act-add"
  }, "+", a.add), a.del && /*#__PURE__*/React.createElement("span", {
    className: "act-del"
  }, "-", a.del)), /*#__PURE__*/React.createElement("span", {
    className: "act-when"
  }, relTime(a.timestamp))))));
}
function CreateSessionDialog({
  open,
  onClose,
  onCreate,
  projects
}) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [projectId, setProjectId] = useState(projects[0]?.id);
  const [picked, setPicked] = useState(["coder"]);
  if (!open) return null;
  const all = window.DEUCE.AGENTS;
  const toggle = k => setPicked(p => p.includes(k) ? p.filter(x => x !== k) : [...p, k]);
  const submit = () => {
    if (!name.trim()) return;
    onCreate({
      name: name.trim().replace(/\s+/g, "-").toLowerCase(),
      description: desc.trim(),
      projectId,
      agentKeys: picked
    });
    setName("");
    setDesc("");
    setPicked(["coder"]);
  };
  return /*#__PURE__*/React.createElement("div", {
    className: "overlay",
    onMouseDown: e => e.target === e.currentTarget && onClose()
  }, /*#__PURE__*/React.createElement("div", {
    className: "dialog"
  }, /*#__PURE__*/React.createElement("h2", null, "New session"), /*#__PURE__*/React.createElement("p", {
    className: "sub"
  }, "A session is a channel for one piece of work, backed by its own dev container."), /*#__PURE__*/React.createElement("div", {
    className: "field"
  }, /*#__PURE__*/React.createElement("label", null, "Project"), /*#__PURE__*/React.createElement("select", {
    value: projectId,
    onChange: e => setProjectId(e.target.value)
  }, projects.map(p => /*#__PURE__*/React.createElement("option", {
    key: p.id,
    value: p.id
  }, p.name)))), /*#__PURE__*/React.createElement("div", {
    className: "field"
  }, /*#__PURE__*/React.createElement("label", null, "Session name"), /*#__PURE__*/React.createElement("input", {
    autoFocus: true,
    value: name,
    onChange: e => setName(e.target.value),
    placeholder: "e.g. payments-webhook",
    onKeyDown: e => e.key === "Enter" && submit()
  })), /*#__PURE__*/React.createElement("div", {
    className: "field"
  }, /*#__PURE__*/React.createElement("label", null, "Description"), /*#__PURE__*/React.createElement("input", {
    value: desc,
    onChange: e => setDesc(e.target.value),
    placeholder: "What's this session for?"
  })), /*#__PURE__*/React.createElement("div", {
    className: "field"
  }, /*#__PURE__*/React.createElement("label", null, "Agents"), /*#__PURE__*/React.createElement("div", {
    className: "agent-pick"
  }, Object.entries(all).map(([k, a]) => {
    const on = picked.includes(k);
    return /*#__PURE__*/React.createElement("button", {
      key: k,
      className: `agent-toggle ${on ? "on" : ""}`,
      onClick: () => toggle(k),
      style: on ? {
        background: a.color,
        borderColor: a.color
      } : {}
    }, /*#__PURE__*/React.createElement("span", {
      className: "d",
      style: {
        background: on ? "#fff" : a.color
      }
    }), a.name);
  }))), /*#__PURE__*/React.createElement("div", {
    className: "dialog-foot"
  }, /*#__PURE__*/React.createElement("button", {
    className: "btn btn-ghost",
    onClick: onClose
  }, "Cancel"), /*#__PURE__*/React.createElement("button", {
    className: "btn btn-primary",
    disabled: !name.trim(),
    onClick: submit
  }, /*#__PURE__*/React.createElement(Icon, {
    name: "Plus"
  }), "Create session"))));
}
Object.assign(window, {
  Sidebar,
  SummaryPanel,
  CreateSessionDialog
});
})(); } catch (e) { __ds_ns.__errors.push({ path: "ui_kits/app/panels.jsx", error: String((e && e.message) || e) }); }

})();
