import { useEffect } from "react";
import { TooltipProvider } from "@/components/ui/tooltip";
import { AppShell } from "@/components/layout/AppShell";
import { useSessionStore } from "@/stores/session-store";
import { seedStore } from "@/mocks/data/seed";

export function App() {
  const store = useSessionStore();

  useEffect(() => {
    if (store.sessions.length === 0) {
      seedStore(store);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <TooltipProvider>
      <div className="dark h-screen w-screen overflow-hidden bg-background text-foreground">
        <AppShell />
      </div>
    </TooltipProvider>
  );
}
