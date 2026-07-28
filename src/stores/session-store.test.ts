import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "@/types";

// Mock the API layer so the store's archive actions don't hit the network.
// Each test configures the resolved values it needs.
vi.mock("@/lib/api", () => ({
  api: {
    archiveSession: vi.fn(async () => undefined),
    unarchiveSession: vi.fn(async () => undefined),
    listArchivedSessions: vi.fn(async () => []),
  },
}));

import { useSessionStore } from "./session-store";
import { api } from "@/lib/api";

function session(overrides: Partial<Session>): Session {
  return {
    id: "s1",
    name: "alpha",
    description: "",
    projectId: "p1",
    status: "active",
    members: [],
    unreadCount: 0,
    createdAt: "2026-06-17T00:00:00Z",
    lastActivityAt: "2026-06-17T00:00:00Z",
    workspaceStatus: "ready",
    planContent: "",
    ...overrides,
  };
}

const initial = useSessionStore.getState();

beforeEach(() => {
  vi.clearAllMocks();
  // Reset the slices these tests touch.
  useSessionStore.setState({
    sessions: [],
    archivedSessions: [],
    activeSessionId: null,
  });
  (api.listArchivedSessions as ReturnType<typeof vi.fn>).mockResolvedValue([]);
});

describe("session-store archive actions", () => {
  it("archiveSession removes the session from the sidebar list", async () => {
    useSessionStore.setState({
      sessions: [session({ id: "s1" }), session({ id: "s2", name: "beta" })],
    });

    await initial.archiveSession("s1");

    const ids = useSessionStore.getState().sessions.map((s) => s.id);
    expect(ids).toEqual(["s2"]);
    expect(api.archiveSession).toHaveBeenCalledWith("s1");
  });

  it("archiveSession clears activeSessionId when the active session is archived", async () => {
    useSessionStore.setState({
      sessions: [session({ id: "s1" })],
      activeSessionId: "s1",
    });

    await initial.archiveSession("s1");

    expect(useSessionStore.getState().activeSessionId).toBeNull();
  });

  it("archiveSession leaves activeSessionId intact when a different session is archived", async () => {
    useSessionStore.setState({
      sessions: [session({ id: "s1" }), session({ id: "s2" })],
      activeSessionId: "s2",
    });

    await initial.archiveSession("s1");

    expect(useSessionStore.getState().activeSessionId).toBe("s2");
  });

  it("archiveSession on an unknown session leaves the list unchanged (idempotent)", async () => {
    useSessionStore.setState({ sessions: [session({ id: "s1" })] });

    await initial.archiveSession("does-not-exist");

    expect(useSessionStore.getState().sessions.map((s) => s.id)).toEqual([
      "s1",
    ]);
  });

  it("restoreSession removes the session from the archived list", async () => {
    useSessionStore.setState({
      archivedSessions: [
        session({ id: "s1", status: "archived" }),
        session({ id: "s2", status: "archived" }),
      ],
    });

    await initial.restoreSession("s1");

    const ids = useSessionStore.getState().archivedSessions.map((s) => s.id);
    expect(ids).toEqual(["s2"]);
    expect(api.unarchiveSession).toHaveBeenCalledWith("s1");
  });

  it("loadArchivedSessions populates archivedSessions from the API", async () => {
    (api.listArchivedSessions as ReturnType<typeof vi.fn>).mockResolvedValue([
      session({ id: "a1", status: "archived" }),
    ]);

    await initial.loadArchivedSessions();

    expect(useSessionStore.getState().archivedSessions.map((s) => s.id)).toEqual(
      ["a1"],
    );
  });
});
