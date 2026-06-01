import { MessageSquare, FileText, FolderTree, Terminal, ScrollText, Code, Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { cn } from "@/lib/utils";
import { useSessionStore } from "@/stores/session-store";
import { ChatView } from "@/components/chat/ChatView";
import { PlanView } from "@/components/plan/PlanView";
import { FilesView } from "@/components/files/FilesView";
import { TerminalView } from "@/components/terminal/TerminalView";
import { LogsView } from "@/components/logs/LogsView";
import { SSHKeySetupModal } from "@/components/session/SSHKeySetupModal";
import { RecoveryCard } from "@/components/workspace/RecoveryCard";
import { WorkspaceMenu } from "@/components/workspace/WorkspaceMenu";
import { api, ApiError } from "@/lib/api";
import type { TabType, WorkspaceStatus } from "@/types";

type Tab = {
  id: TabType;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  // requiresLiveWorkspace=true tabs fade and become unclickable when the
  // workspace isn't ready/starting. Plan is DB-backed (planContent on the
  // session row) so it stays interactive regardless of container state.
  requiresLiveWorkspace: boolean;
};

const tabs: Tab[] = [
  { id: "chat", label: "Chat", icon: MessageSquare, requiresLiveWorkspace: true },
  { id: "plan", label: "Plan", icon: FileText, requiresLiveWorkspace: false },
  { id: "files", label: "Files", icon: FolderTree, requiresLiveWorkspace: true },
  { id: "terminal", label: "Terminal", icon: Terminal, requiresLiveWorkspace: true },
];

function isWorkspaceLive(status: WorkspaceStatus | undefined): boolean {
  return status === "ready" || status === "starting";
}

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

// Hide the "Open in VS Code" button on mobile browsers. Per R13, the
// vscode:// URI handoff doesn't work on mobile. UA-sniff (not viewport)
// because a narrowed desktop browser should still show the button.
function isMobileUA(): boolean {
  if (typeof navigator === "undefined") return false;
  return /Android|iPhone|iPad|iPod|Opera Mini|IEMobile|Mobile/i.test(navigator.userAgent);
}

export function CenterPanel() {
  const { activeSessionId, activeTabMap, setActiveTab, sessions, showLogs, setShowLogs, workspaceLogs } =
    useSessionStore();
  const [vscodeLoading, setVscodeLoading] = useState(false);
  const [sshKeyModalOpen, setSshKeyModalOpen] = useState(false);
  const [vscodeError, setVscodeError] = useState<string | null>(null);

  const activeSession = activeSessionId
    ? sessions.find((s) => s.id === activeSessionId)
    : undefined;
  const activeTab = activeSessionId
    ? activeTabMap[activeSessionId] ?? "chat"
    : "chat";
  const workspaceStatus = activeSession?.workspaceStatus;
  const live = isWorkspaceLive(workspaceStatus);

  // When the workspace is not live, force-switch the active tab away from one
  // that requires the live workspace, so the user doesn't land in a
  // workspace-required tab and find the recovery card with the wrong tab
  // visually selected. Plan is always safe. Effect must run unconditionally
  // (hooks rule); the guard inside handles the null-session case.
  useEffect(() => {
    if (!activeSessionId || live) return;
    const currentTab = tabs.find((t) => t.id === activeTab);
    if (currentTab?.requiresLiveWorkspace) {
      setActiveTab(activeSessionId, "plan");
    }
  }, [activeSessionId, activeTab, live, setActiveTab]);

  if (!activeSessionId) {
    return <EmptyState />;
  }

  const hasLogs = (workspaceLogs[activeSessionId]?.length ?? 0) > 0;
  const isBuilding =
    workspaceStatus === "starting" ||
    workspaceStatus === "stopping" ||
    workspaceStatus === "rebuilding" ||
    workspaceStatus === "deleting";
  const workspaceReady = workspaceStatus === "ready";
  const showVSCodeButton = !isMobileUA();

  async function handleOpenInVSCode() {
    if (!activeSessionId || vscodeLoading) return;
    setVscodeLoading(true);
    setVscodeError(null);
    try {
      const { uri } = await api.getSessionVSCodeURI(activeSessionId);
      window.location.href = uri;
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === "NO_SSH_KEY") {
          setSshKeyModalOpen(true);
        } else if (err.code === "SSH_UNAVAILABLE") {
          setVscodeError("VS Code remote access isn't available. Contact your administrator.");
        } else {
          setVscodeError("Couldn't open VS Code. Try again.");
        }
      } else {
        setVscodeError("Couldn't open VS Code. Try again.");
      }
    } finally {
      setVscodeLoading(false);
    }
  }

  return (
    <div className="flex h-full flex-col bg-background">
      {/* Session header + status */}
      {activeSession && (
        <div className="flex flex-col gap-0.5 border-b border-border-muted px-4 py-2">
          <div className="flex items-center gap-2">
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
          <div className="flex items-center justify-between">
            {activeSession.description && (
              <span className="truncate text-xs text-foreground-muted">
                {activeSession.description}
              </span>
            )}
            <div className="ml-auto shrink-0">
              <WorkspaceMenu session={activeSession} />
            </div>
          </div>
        </div>
      )}

      {/* Tab bar */}
      <div className="flex border-b border-border-muted">
        {tabs.map((tab) => {
          const Icon = tab.icon;
          const isActive = activeTab === tab.id && !showLogs;
          const tabDisabled = tab.requiresLiveWorkspace && !live;
          const disabledTitle = tabDisabled
            ? workspaceStatus
              ? `Workspace is ${workspaceStatus} — start or rebuild to use this tab`
              : "Workspace not ready"
            : undefined;
          return (
            <button
              key={tab.id}
              onClick={() => {
                if (tabDisabled) return;
                setActiveTab(activeSessionId, tab.id);
                setShowLogs(false);
              }}
              aria-disabled={tabDisabled || undefined}
              title={disabledTitle}
              className={cn(
                "flex items-center gap-1.5 border-b-2 px-4 py-2 text-sm transition-colors",
                isActive
                  ? "border-accent text-foreground-emphasis"
                  : "border-transparent text-foreground-muted hover:text-foreground",
                tabDisabled && "cursor-not-allowed opacity-50 hover:text-foreground-muted",
              )}
            >
              <Icon className="h-4 w-4" />
              {tab.label}
            </button>
          );
        })}

        {/* Open in VS Code — pushed right with ml-auto on the FIRST right-side button */}
        {showVSCodeButton && (
          <button
            data-vscode-button
            onClick={handleOpenInVSCode}
            disabled={vscodeLoading || !workspaceReady}
            aria-busy={vscodeLoading}
            title={!workspaceReady ? "Container not ready" : vscodeError ?? "Open this session in VS Code"}
            className={cn(
              "ml-auto flex items-center gap-1.5 border-b-2 border-transparent px-4 py-2 text-sm transition-colors",
              "text-foreground-muted hover:text-foreground",
              "disabled:cursor-not-allowed disabled:opacity-50",
            )}
          >
            {vscodeLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Code className="h-4 w-4" />
            )}
            VS Code
          </button>
        )}

        {/* Logs icon — to the right of VS Code (or ml-auto when VS Code is hidden) */}
        <button
          onClick={() => setShowLogs(!showLogs)}
          className={cn(
            "flex items-center gap-1.5 border-b-2 px-4 py-2 text-sm transition-colors",
            !showVSCodeButton && "ml-auto",
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
            {activeTab === "plan" && <PlanView />}
            {/* The other three tabs require a live container. When the
                workspace isn't live, the recovery card replaces the panel
                entirely — no spinner, no error message, just a single
                clear next-action surface. */}
            {activeTab === "chat" &&
              (live ? <ChatView /> : activeSession && <RecoveryCard session={activeSession} />)}
            {activeTab === "files" &&
              (live ? <FilesView key={activeSessionId} /> : activeSession && <RecoveryCard session={activeSession} />)}
            {activeTab === "terminal" &&
              (live ? <TerminalView /> : activeSession && <RecoveryCard session={activeSession} />)}
          </>
        )}
      </div>

      {/* SSH key setup modal — fired by handleOpenInVSCode when /vscode-uri
          returns 412 NO_SSH_KEY. The modal handles the create-key flow and
          navigates to the vscode:// URI itself on success. */}
      {showVSCodeButton && (
        <SSHKeySetupModal
          sessionID={activeSessionId}
          open={sshKeyModalOpen}
          onClose={() => setSshKeyModalOpen(false)}
        />
      )}
    </div>
  );
}
