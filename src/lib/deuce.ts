// The single built-in agent. The id MUST match agent.DeuceAgentID in
// server/internal/agent/store.go and the row seeded by migration
// 013_single_deuce_agent.sql — message authorship, the chat visibility
// filter, and historical repoints all pin to it.
export const DEUCE = {
  id: "00000000-0000-0000-0000-00000000000d",
  name: "deuce",
  // Accent + muted variants (former Coder blue — the channel's one agent color).
  color: "#58a6ff",
  colorMuted: "#0c2d6b",
} as const;

// The nil UUID is the system-notice author sentinel (authorType "agent" with
// this id renders as a system notice and stays visible in chat).
export const SYSTEM_AUTHOR_ID = "00000000-0000-0000-0000-000000000000";
