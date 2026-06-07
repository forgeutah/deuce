import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { SessionSidebar } from "./SessionSidebar";
import { CenterPanel } from "./CenterPanel";
import { SummaryPanel } from "./SummaryPanel";
import { ThreadDrawerPanel } from "@/components/super-threads/ThreadDrawerPanel";
import { useSessionStore } from "@/stores/session-store";

// Clear any stale cached panel layouts from localStorage
for (const key of Object.keys(localStorage)) {
  if (key.startsWith("react-resizable-panels:")) {
    localStorage.removeItem(key);
  }
}

export function AppShell() {
  // When an agent thread is open, the right panel swaps the summary view for the
  // Super Threads drawer (the drawer carries its own chrome + agent-colored
  // border, so it replaces rather than stacks on the summary).
  const threadOpen = useSessionStore((s) => s.openThread !== null);

  return (
    <ResizablePanelGroup orientation="horizontal" className="h-full">
      <ResizablePanel
        defaultSize={20}
        minSize={15}
        className="bg-background-subtle"
      >
        <SessionSidebar />
      </ResizablePanel>

      <ResizableHandle withHandle />

      <ResizablePanel defaultSize={55} minSize={30}>
        <CenterPanel />
      </ResizablePanel>

      <ResizableHandle withHandle />

      <ResizablePanel
        defaultSize={25}
        minSize={15}
        className={threadOpen ? undefined : "bg-background-subtle"}
      >
        {threadOpen ? <ThreadDrawerPanel /> : <SummaryPanel />}
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
