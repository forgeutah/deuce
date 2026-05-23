import { useEffect, useState } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AppShell } from "@/components/layout/AppShell";
import { NotAuthorizedView } from "@/components/auth/NotAuthorizedView";
import { useSessionStore } from "@/stores/session-store";
import { useWebSocket } from "@/hooks/use-websocket";
import { api, ApiError } from "@/lib/api";
import { Loader2 } from "lucide-react";

function AppContent() {
  useWebSocket();
  return <AppShell />;
}

export function App() {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [notAuthorized, setNotAuthorized] = useState(false);

  useEffect(() => {
    loadData();
  }, []);

  async function loadData() {
    const store = useSessionStore.getState();

    // /api/me runs in its own try/catch so a 403 here — meaning "this user
    // is not allowed in Deuce at all" — triggers the dedicated
    // NotAuthorizedView. 403s from any other endpoint (e.g. a future
    // per-session permission gate) flow through generic error handling and
    // must NOT take over the whole shell.
    let me: Awaited<ReturnType<typeof api.getMe>>;
    try {
      me = await api.getMe();
    } catch (err: unknown) {
      console.error("Failed to load /api/me:", err);
      if (err instanceof ApiError && err.code === "NOT_AUTHORIZED") {
        setNotAuthorized(true);
        setLoading(false);
        return;
      }
      const message =
        err instanceof Error ? err.message : "Failed to connect to server";
      setError(message);
      setLoading(false);
      return;
    }
    store.setCurrentUser(me);

    try {
      const [teams, projects, sessions] = await Promise.all([
        api.listTeams(),
        api.listProjects(),
        api.listSessions(),
      ]);

      store.setTeams(teams);
      store.setProjects(projects);
      store.setSessions(sessions);

      // Set first active session
      const firstActive = sessions.find(
        (s: any) => s.status === "active",
      );
      if (firstActive) {
        store.setActiveSession(firstActive.id);

        // Load messages and activities for the first session
        const [msgData, activities] = await Promise.all([
          api.listMessages(firstActive.id),
          api.listActivities(firstActive.id),
        ]);
        store.setMessages(firstActive.id, msgData.messages.reverse());
        store.setActivities(firstActive.id, activities);
      }

      setLoading(false);
    } catch (err: unknown) {
      console.error("Failed to load app data:", err);
      const message =
        err instanceof Error ? err.message : "Failed to load app data";
      setError(message);
      setLoading(false);
    }
  }

  function retry() {
    setError(null);
    setNotAuthorized(false);
    setLoading(true);
    loadData();
  }

  if (notAuthorized) {
    return <NotAuthorizedView onRetry={retry} />;
  }

  if (loading) {
    return (
      <div className="dark flex h-screen w-screen items-center justify-center bg-background text-foreground">
        <div className="flex flex-col items-center gap-3">
          <Loader2 className="h-8 w-8 animate-spin text-accent" />
          <p className="text-sm text-foreground-muted">Connecting to Deuce...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="dark flex h-screen w-screen items-center justify-center bg-background text-foreground">
        <div className="flex flex-col items-center gap-3 text-center">
          <p className="text-sm text-danger">{error}</p>
          <p className="text-xs text-foreground-muted">
            Make sure the Go server is running on :8080
          </p>
          <button
            onClick={retry}
            className="mt-2 rounded-md bg-accent-emphasis px-4 py-1.5 text-sm text-foreground-on-emphasis hover:bg-accent"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <TooltipProvider>
      <div className="dark h-screen w-screen overflow-hidden bg-background text-foreground">
        <AppContent />
      </div>
    </TooltipProvider>
  );
}
