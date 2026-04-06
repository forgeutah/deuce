import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@/components/ui/resizable";
import { SessionSidebar } from "./SessionSidebar";
import { CenterPanel } from "./CenterPanel";
import { SummaryPanel } from "./SummaryPanel";

// Clear any stale cached panel layouts from localStorage
for (const key of Object.keys(localStorage)) {
  if (key.startsWith("react-resizable-panels:")) {
    localStorage.removeItem(key);
  }
}

export function AppShell() {
  return (
    <ResizablePanelGroup direction="horizontal" className="h-full">
      <ResizablePanel
        defaultSize={20}
        minSize={15}
        className="bg-background-subtle"
        order={1}
      >
        <SessionSidebar />
      </ResizablePanel>

      <ResizableHandle withHandle />

      <ResizablePanel defaultSize={55} minSize={30} order={2}>
        <CenterPanel />
      </ResizablePanel>

      <ResizableHandle withHandle />

      <ResizablePanel
        defaultSize={25}
        minSize={15}
        className="bg-background-subtle"
        order={3}
      >
        <SummaryPanel />
      </ResizablePanel>
    </ResizablePanelGroup>
  );
}
