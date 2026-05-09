import { useEffect, useState } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AppShell } from "@/components/layout/AppShell";
import { useSessionStore } from "@/stores/session-store";
import { useWebSocket } from "@/hooks/use-websocket";
import { api } from "@/lib/api";
import { Loader2 } from "lucide-react";

function AppContent() {
  useWebSocket();
  return <AppShell />;
}

export function App() {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadData();
  }, []);

  async function loadData() {
    try {
      const store = useSessionStore.getState();

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
    } catch (err: any) {
      console.error("Failed to load data:", err);
      setError(err.message ?? "Failed to connect to server");
      setLoading(false);
    }
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
          <p className="text-xs text-foreground-subtle">
            Make sure the Go server is running on :8080
          </p>
          <button
            onClick={() => {
              setError(null);
              setLoading(true);
              loadData();
            }}
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
