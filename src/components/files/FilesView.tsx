import { useState } from "react";
import {
  File,
  Folder,
  FolderOpen,
  ChevronRight,
  ChevronDown,
  Loader2,
  AlertCircle,
} from "lucide-react";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { useSessionStore } from "@/stores/session-store";
import type { FileNode } from "@/types";

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
            <File className="h-3.5 w-3.5 text-foreground-subtle shrink-0" />
          </>
        )}
        <span className="truncate text-foreground">{node.name}</span>
        {node.modifiedBy && (
          <span className="ml-auto h-2 w-2 shrink-0 rounded-full bg-accent" />
        )}
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
  const { activeSessionId, sessions, fileTrees } = useSessionStore();
  const [selectedFile, setSelectedFile] = useState<FileNode | null>(null);

  const session = sessions.find((s) => s.id === activeSessionId);
  const files = activeSessionId ? (fileTrees[activeSessionId] ?? []) : [];

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

  return (
    <div className="flex h-full">
      {/* File tree */}
      <ScrollArea className="w-64 shrink-0 border-r border-border-muted p-2">
        {files.map((node) => (
          <FileTreeItem
            key={node.path}
            node={node}
            depth={0}
            selectedPath={selectedFile?.path ?? null}
            onSelect={setSelectedFile}
          />
        ))}
      </ScrollArea>

      {/* Code viewer */}
      <div className="flex-1 overflow-hidden">
        {selectedFile ? (
          <div className="flex h-full flex-col">
            <div className="flex items-center gap-2 border-b border-border-muted bg-background-subtle px-3 py-1.5">
              <File className="h-3.5 w-3.5 text-foreground-subtle" />
              <span className="text-xs text-foreground-emphasis font-medium">
                {selectedFile.path}
              </span>
              {selectedFile.modifiedBy && (
                <span className="text-[10px] text-accent">modified by agent</span>
              )}
            </div>
            <ScrollArea className="flex-1 bg-background-inset">
              <pre className="p-4 font-mono text-[13px] leading-5 text-foreground">
                {selectedFile.content ?? "// File content would appear here"}
              </pre>
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
