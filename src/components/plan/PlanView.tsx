import { useState, useRef, useCallback } from "react";
import { FileText, Eye, Split, Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";
import { useSessionStore } from "@/stores/session-store";

type ViewMode = "split" | "editor" | "preview";

export function PlanView() {
  const { activeSessionId, sessions, updateSessionPlan } = useSessionStore();
  const [viewMode, setViewMode] = useState<ViewMode>("split");
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  const session = sessions.find((s) => s.id === activeSessionId);
  const content = session?.planContent ?? "";

  if (!session) return null;

  const isReadOnly = session.status !== "active";

  const handleChange = useCallback(
    (value: string) => {
      if (!activeSessionId) return;
      // Update local state immediately
      updateSessionPlan(activeSessionId, value);

      // Debounce API call
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        api.updateSession(activeSessionId, { planContent: value }).catch(console.error);
      }, 500);
    },
    [activeSessionId, updateSessionPlan],
  );

  return (
    <div className="flex h-full flex-col">
      {/* Toolbar */}
      <div className="flex items-center justify-between border-b border-border-muted px-3 py-1.5">
        <span className="text-xs text-foreground-subtle">Plan Document</span>
        <div className="flex gap-0.5">
          <Button
            variant="ghost"
            size="icon"
            className={cn("h-7 w-7", viewMode === "editor" && "bg-background-emphasis")}
            onClick={() => setViewMode("editor")}
          >
            <Pencil className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className={cn("h-7 w-7", viewMode === "split" && "bg-background-emphasis")}
            onClick={() => setViewMode("split")}
          >
            <Split className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            className={cn("h-7 w-7", viewMode === "preview" && "bg-background-emphasis")}
            onClick={() => setViewMode("preview")}
          >
            <Eye className="h-3.5 w-3.5" />
          </Button>
        </div>
      </div>

      {/* Content */}
      <div className="flex flex-1 overflow-hidden">
        {/* Editor */}
        {(viewMode === "editor" || viewMode === "split") && (
          <div className={cn("flex-1 overflow-hidden", viewMode === "split" && "border-r border-border-muted")}>
            {content || !isReadOnly ? (
              <textarea
                value={content}
                onChange={(e) => !isReadOnly && handleChange(e.target.value)}
                readOnly={isReadOnly}
                className="h-full w-full resize-none bg-background-inset p-4 font-mono text-sm text-foreground placeholder:text-foreground-subtle focus:outline-none"
                placeholder="Start writing your plan..."
              />
            ) : (
              <div className="flex h-full items-center justify-center">
                <div className="text-center">
                  <FileText className="mx-auto h-8 w-8 text-foreground-subtle mb-2" />
                  <p className="text-sm text-foreground-muted">No plan yet</p>
                  <p className="text-xs text-foreground-subtle mt-1">
                    Start writing to define what this session should accomplish.
                  </p>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Preview */}
        {(viewMode === "preview" || viewMode === "split") && (
          <ScrollArea className="flex-1">
            <div className="p-4 prose prose-invert prose-sm max-w-none">
              {content ? (
                <pre className="whitespace-pre-wrap font-sans text-sm text-foreground">
                  {content}
                </pre>
              ) : (
                <p className="text-foreground-subtle italic">Nothing to preview yet.</p>
              )}
            </div>
          </ScrollArea>
        )}
      </div>
    </div>
  );
}
