import type { Session } from "@/types";

// isSessionMember is the single source of truth for "has this user joined this
// session" — the authorization predicate the UI gates posting on. Session
// membership (distinct from team membership, which only grants read access)
// unlocks the composer, agent steering, and the live event stream.
export function isSessionMember(
  session: Session,
  userId: string | undefined,
): boolean {
  if (!userId) return false;
  return session.members.some((m) => m.id === userId);
}
