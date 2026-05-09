import { MessageSquare, FileText, FolderTree, Terminal, ScrollText } from "lucide-react";
import { cn } from "@/lib/utils";
import { useSessionStore } from "@/stores/session-store";
import { ChatView } from "@/components/chat/ChatView";
import { PlanView } from "@/components/plan/PlanView";
import { FilesView } from "@/components/files/FilesView";
import { TerminalView } from "@/components/terminal/TerminalView";
import { LogsView } from "@/components/logs/LogsView";
import type { TabType } from "@/types";

const tabs: { id: TabType; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { id: "chat", label: "Chat", icon: MessageSquare },
  { id: "plan", label: "Plan", icon: FileText },
  { id: "files", label: "Files", icon: FolderTree },
  { id: "terminal", label: "Terminal", icon: Terminal },
];

function EmptyState() {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="text-center">
        <h2 className="text-lg font-semibold text-foreground-emphasis">
          Welcome to Deuce
        </h2>
        <p className="mt-2 text-sm text-foreground-muted">
          Select a session from the sidebar or create a new one to get started.
        </p>
      </div>
    </div>
  );
}

export function CenterPanel() {
  const { activeSessionId, activeTabMap, setActiveTab, sessions, showLogs, setShowLogs, workspaceLogs } =
    useSessionStore();

  if (!activeSessionId) {
    return <EmptyState />;
  }

  const activeSession = sessions.find((s) => s.id === activeSessionId);
  const activeTab = activeTabMap[activeSessionId] ?? "chat";
  const hasLogs = (workspaceLogs[activeSessionId]?.length ?? 0) > 0;
  const isBuilding = activeSession?.workspaceStatus === "starting";

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Session header + status */}
      {activeSession && (
        <div className="flex items-center gap-2 border-b border-border-muted px-4 py-2">
          <span className="text-sm font-semibold text-foreground-emphasis">
            # {activeSession.name}
          </span>
          {activeSession.status !== "active" && (
            <span
              className={cn(
                "rounded-full px-2 py-0.5 text-[10px] font-medium",
                activeSession.status === "paused"
                  ? "bg-warning-muted text-warning"
                  : "bg-neutral-6 text-foreground-muted",
              )}
            >
              {activeSession.status === "paused" ? "Paused" : "Archived"}
            </span>
          )}
        </div>
      )}

      {/* Tab bar */}
      <div className="flex border-b border-border-muted">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id && !showLogs;
          return (
            <button
              key={tab.id}
              onClick={() => {
                setActiveTab(activeSessionId, tab.id);
                setShowLogs(false);
              }}
              className={cn(
                "flex items-center gap-1.5 border-b-2 px-4 py-2 text-sm transition-colors",
                isActive
                  ? "border-accent text-foreground-emphasis"
                  : "border-transparent text-foreground-muted hover:text-foreground",
              )}
            >
              <Icon className="h-4 w-4" />
              {tab.label}
            </button>
          );
        })}

        {/* Logs icon — far right */}
        <button
          onClick={() => setShowLogs(!showLogs)}
          className={cn(
            "ml-auto flex items-center gap-1.5 border-b-2 px-4 py-2 text-sm transition-colors",
            showLogs
              ? "border-accent text-foreground-emphasis"
              : "border-transparent text-foreground-muted hover:text-foreground",
          )}
        >
          <div className="relative">
            <ScrollText className="h-4 w-4" />
            {isBuilding && (
              <span className="absolute -right-1 -top-1 h-2 w-2 rounded-full bg-warning animate-pulse-dot" />
            )}
            {!isBuilding && hasLogs && (
              <span className="absolute -right-1 -top-1 h-1.5 w-1.5 rounded-full bg-foreground-subtle" />
            )}
          </div>
          Logs
        </button>
      </div>

      {/* Tab content */}
      <div className="flex-1 overflow-hidden">
        {showLogs ? (
          <LogsView />
        ) : (
          <>
            {activeTab === "chat" && <ChatView />}
            {activeTab === "plan" && <PlanView />}
            {activeTab === "files" && <FilesView />}
            {activeTab === "terminal" && <TerminalView />}
          </>
        )}
      </div>
    </div>
  );
}
