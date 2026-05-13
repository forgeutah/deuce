import { useEffect, useRef, useState } from "react";
import {
  File,
  Folder,
  FolderOpen,
  ChevronRight,
  ChevronDown,
  Loader2,
  AlertCircle,
  RotateCw,
} from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { useSessionStore } from "@/stores/session-store";
import { api } from "@/lib/api";
import type { FileNode, FileContentResponse, GitStatus } from "@/types";

const STATUS_COLOR: Record<GitStatus, string> = {
  M: "text-warning",
  U: "text-success",
  A: "text-accent",
  D: "text-danger",
};

// Content fetch state. The fetch state is keyed by the path (and session) it
// describes, so the renderer can distinguish "loading new file" from
// "showing previous file's data while new fetch is in flight."
type ContentState =
  | { status: "idle" }
  | {
      status: "success";
      path: string;
      sessionId: string;
      data: FileContentResponse;
    }
  | {
      status: "error";
      path: string;
      sessionId: string;
      message: string;
    };

function FileTreeItem({
  node,
  depth,
  selectedPath,
  onSelect,
}: {
  node: FileNode;
  depth: number;
  selectedPath: string | null;
  onSelect: (node: FileNode) => void;
}) {
  const [isOpen, setIsOpen] = useState(depth < 1);
  const isDir = node.type === "directory";
  const isSelected = node.path === selectedPath;
  const statusColor = node.gitStatus ? STATUS_COLOR[node.gitStatus] : undefined;
  const isDeleted = node.gitStatus === "D";

  return (
    <div>
      <button
        onClick={() => {
          if (isDir) setIsOpen(!isOpen);
          else onSelect(node);
        }}
        className={cn(
          "flex w-full items-center gap-1 rounded-sm px-1 py-0.5 text-xs hover:bg-background-hover",
          isSelected && "bg-background-emphasis text-foreground-emphasis",
        )}
        style={{ paddingLeft: `${depth * 16 + 4}px` }}
      >
        {isDir ? (
          <>
            {isOpen ? (
              <ChevronDown className="h-3 w-3 text-foreground-subtle shrink-0" />
            ) : (
              <ChevronRight className="h-3 w-3 text-foreground-subtle shrink-0" />
            )}
            {isOpen ? (
              <FolderOpen className="h-3.5 w-3.5 text-accent shrink-0" />
            ) : (
              <Folder className="h-3.5 w-3.5 text-accent shrink-0" />
            )}
          </>
        ) : (
          <>
            <span className="w-3 shrink-0" />
            <File
              className={cn(
                "h-3.5 w-3.5 shrink-0",
                statusColor ?? "text-foreground-subtle",
              )}
            />
          </>
        )}
        <span
          className={cn(
            "truncate text-foreground",
            !isDir && statusColor,
            isDeleted && "line-through",
          )}
        >
          {node.name}
        </span>
        {isDir && node.isRepoRoot && (
          <span className="ml-1 shrink-0 text-[10px] text-foreground-subtle font-mono">
            [repo]
          </span>
        )}
        {/* Status badge slot — fixed width so names don't reflow when status appears */}
        <span
          className={cn(
            "ml-auto w-4 shrink-0 text-center font-mono text-[10px] font-semibold",
            statusColor,
          )}
        >
          {!isDir && node.gitStatus ? node.gitStatus : ""}
        </span>
        {/* Agent-modified dot slot — present even when empty to prevent name reflow */}
        <span className="w-2 shrink-0">
          {node.modifiedBy && (
            <span
              className="block h-2 w-2 rounded-full bg-accent"
              title={`Modified by ${node.modifiedBy}`}
            />
          )}
        </span>
      </button>
      {isDir && isOpen && node.children && (
        <div>
          {node.children.map((child) => (
            <FileTreeItem
              key={child.path}
              node={child}
              depth={depth + 1}
              selectedPath={selectedPath}
              onSelect={onSelect}
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function FilesView() {
  const { activeSessionId, sessions, fileTrees, refreshFiles } =
    useSessionStore();
  const [selectedFile, setSelectedFile] = useState<FileNode | null>(null);
  const [contentState, setContentState] = useState<ContentState>({
    status: "idle",
  });
  const [refreshing, setRefreshing] = useState(false);
  const [refreshError, setRefreshError] = useState(false);

  // Monotonic request counter — guards against stale .then callbacks from
  // aborted-but-already-dispatched fetches resolving out of order.
  const requestSeqRef = useRef(0);
  const refreshErrorTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );

  const session = sessions.find((s) => s.id === activeSessionId);
  const files = activeSessionId ? fileTrees[activeSessionId] : undefined;

  // Fetch content when selection changes. Cancellation is handled by both the
  // AbortController (network-level) and the request seq (state-level).
  useEffect(() => {
    if (!selectedFile || !activeSessionId) return;
    const mySeq = ++requestSeqRef.current;
    const path = selectedFile.path;
    const sessionId = activeSessionId;
    const controller = new AbortController();

    api
      .getFileContent(sessionId, path, controller.signal)
      .then((resp) => {
        if (mySeq !== requestSeqRef.current) return;
        setContentState({ status: "success", path, sessionId, data: resp });
      })
      .catch((err: unknown) => {
        if (mySeq !== requestSeqRef.current) return;
        if (err instanceof Error && err.name === "AbortError") return;
        const message = err instanceof Error ? err.message : "Failed to load";
        setContentState({ status: "error", path, sessionId, message });
      });

    return () => controller.abort();
  }, [selectedFile, activeSessionId]);

  useEffect(() => {
    return () => {
      if (refreshErrorTimerRef.current)
        clearTimeout(refreshErrorTimerRef.current);
    };
  }, []);

  const handleRefresh = async () => {
    if (!activeSessionId || refreshing) return;
    setRefreshing(true);
    setRefreshError(false);
    try {
      await refreshFiles(activeSessionId);
    } catch {
      setRefreshError(true);
      if (refreshErrorTimerRef.current)
        clearTimeout(refreshErrorTimerRef.current);
      refreshErrorTimerRef.current = setTimeout(
        () => setRefreshError(false),
        2000,
      );
    } finally {
      setRefreshing(false);
    }
  };

  if (!session) return null;

  if (session.workspaceStatus === "starting") {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <Loader2 className="mx-auto h-8 w-8 text-warning animate-spin mb-3" />
          <p className="text-sm text-foreground-muted">Workspace warming up...</p>
          <p className="text-xs text-foreground-subtle mt-1">
            Files will be available once the workspace is ready.
          </p>
        </div>
      </div>
    );
  }

  if (session.workspaceStatus === "failed") {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <AlertCircle className="mx-auto h-8 w-8 text-danger mb-3" />
          <p className="text-sm text-foreground-muted">Workspace failed to start</p>
          <button className="mt-2 text-xs text-accent hover:underline">
            Retry
          </button>
        </div>
      </div>
    );
  }

  // Derived content-pane state. "loading" = a file is selected but content
  // state doesn't yet match its path/session — avoids needing a synchronous
  // setState in the fetch effect.
  const contentMatchesSelection =
    selectedFile &&
    contentState.status !== "idle" &&
    contentState.path === selectedFile.path &&
    contentState.sessionId === activeSessionId;
  const showContentLoading = !!selectedFile && !contentMatchesSelection;
  const showContentError =
    contentMatchesSelection && contentState.status === "error";
  const showContentSuccess =
    contentMatchesSelection && contentState.status === "success";
  const successData =
    contentState.status === "success" ? contentState.data : null;
  const errorMessage =
    contentState.status === "error" ? contentState.message : null;

  return (
    <div className="flex h-full">
      {/* File tree */}
      <div className="flex w-64 shrink-0 flex-col border-r border-border-muted">
        <div className="flex items-center justify-between border-b border-border-muted bg-background-subtle px-2 py-1">
          <span className="text-[10px] uppercase tracking-wide text-foreground-subtle">
            Files
          </span>
          <button
            onClick={handleRefresh}
            disabled={refreshing || !files}
            aria-label="Refresh file tree"
            className={cn(
              "rounded-sm p-1 text-foreground-subtle hover:bg-background-hover hover:text-foreground",
              (refreshing || !files) && "opacity-50 cursor-not-allowed",
            )}
            title="Refresh"
          >
            {refreshError ? (
              <AlertCircle className="h-3.5 w-3.5 text-danger" />
            ) : (
              <RotateCw
                className={cn("h-3.5 w-3.5", refreshing && "animate-spin")}
              />
            )}
          </button>
        </div>
        <ScrollArea className="flex-1 p-2">
          {files === undefined && (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-4 w-4 animate-spin text-foreground-subtle" />
            </div>
          )}
          {files !== undefined && files.length === 0 && (
            <div className="flex flex-col items-center justify-center py-8 text-center">
              <Folder className="h-5 w-5 text-foreground-subtle mb-2" />
              <p className="text-xs text-foreground-muted">
                No files in workspace
              </p>
            </div>
          )}
          {files !== undefined &&
            files.length > 0 &&
            files.map((node) => (
              <FileTreeItem
                key={node.path}
                node={node}
                depth={0}
                selectedPath={selectedFile?.path ?? null}
                onSelect={setSelectedFile}
              />
            ))}
        </ScrollArea>
      </div>

      {/* Code viewer */}
      <div className="flex-1 overflow-hidden">
        {selectedFile ? (
          <div className="flex h-full flex-col">
            <div className="flex items-center gap-2 border-b border-border-muted bg-background-subtle px-3 py-1.5">
              <File className="h-3.5 w-3.5 text-foreground-subtle" />
              <span className="text-xs text-foreground-emphasis font-medium">
                {selectedFile.path}
              </span>
              {selectedFile.gitStatus && (
                <span
                  className={cn(
                    "text-[10px] font-mono font-semibold",
                    STATUS_COLOR[selectedFile.gitStatus],
                  )}
                >
                  {selectedFile.gitStatus}
                </span>
              )}
              {selectedFile.modifiedBy && (
                <span className="text-[10px] text-accent">
                  modified by {selectedFile.modifiedBy}
                </span>
              )}
            </div>
            {showContentSuccess && successData?.truncated && (
              <div className="border-b border-border-muted bg-warning/10 px-3 py-1 text-[10px] text-warning">
                File truncated to 1 MB (full size:{" "}
                {(successData.size / 1024 / 1024).toFixed(2)} MB)
              </div>
            )}
            <ScrollArea className="flex-1 bg-background-inset">
              {showContentLoading && (
                <div className="flex h-full items-center justify-center p-8">
                  <Loader2 className="h-5 w-5 animate-spin text-foreground-subtle" />
                </div>
              )}
              {showContentError && (
                <div className="flex h-full flex-col items-center justify-center p-8 text-center">
                  <AlertCircle className="h-5 w-5 text-danger mb-2" />
                  <p className="text-sm text-foreground-muted">
                    Failed to load file
                  </p>
                  {errorMessage && (
                    <p className="mt-1 text-xs text-foreground-subtle">
                      {errorMessage}
                    </p>
                  )}
                </div>
              )}
              {showContentSuccess && successData?.isBinary && (
                <div className="flex h-full items-center justify-center p-8 text-center">
                  <p className="text-sm text-foreground-subtle">
                    Binary file — preview unavailable
                  </p>
                </div>
              )}
              {showContentSuccess && successData && !successData.isBinary && (
                <pre className="p-4 font-mono text-[13px] leading-5 text-foreground">
                  {successData.content}
                </pre>
              )}
            </ScrollArea>
          </div>
        ) : (
          <div className="flex h-full items-center justify-center">
            <p className="text-sm text-foreground-subtle">
              Select a file to view its contents
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
