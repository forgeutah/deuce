/* Icon + small helpers. Shared via window. */

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
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

// Render @mentions inside message text with an accent color.
function renderContent(text) {
  const parts = text.split(/(@\w+)/g);
  return parts.map((p, i) =>
    /^@\w+$/.test(p) ? <span className="mention" key={i}>{p}</span> : <React.Fragment key={i}>{p}</React.Fragment>
  );
}

Object.assign(window, { Icon, relTime, clockTime, renderContent });
