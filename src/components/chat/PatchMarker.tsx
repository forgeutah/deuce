import { FileDiff, GitBranch, AlertTriangle } from "lucide-react";
import type { Patch, Agent } from "@/types";
import { cn } from "@/lib/utils";

interface PatchMarkerProps {
  patch: Patch;
  // The agent (if any) that produced the patch. Used for the origin badge
  // color when originType === "agent". Pass undefined for human / system
  // origins or when the producing agent isn't loaded.
  producingAgent?: Agent;
}

// PatchMarker is the v0 read-only chat-surface marker that confirms a patch
// landed for an agent turn. Renders adjacent to the producing message via
// ChatView's interleaving logic. Scope is intentionally minimal — file/hunk
// counts, origin badge, supersession indicator, failed-mid-turn flag. There
// is no click behavior, no expand/collapse, no hover detail: those belong
// to the future Changes view (see plan Scope Boundaries).
export function PatchMarker({ patch, producingAgent }: PatchMarkerProps) {
  const fileLabel = patch.fileCount === 1 ? "file" : "files";
  const hunkLabel = patch.hunkCount === 1 ? "hunk" : "hunks";
  const isAgent = patch.originType === "agent";
  const supersedes = patch.parentPatchId !== null;
  const failed = patch.failedMidTurn;

  // Use the agent's color when we have it; fall back to neutral muted tone
  // for human/system origins and missing-agent cases.
  const badgeColor =
    isAgent && producingAgent ? producingAgent.color : "var(--color-foreground-muted)";
  const originLabel = isAgent
    ? producingAgent?.name ?? "Agent"
    : patch.originType === "human"
      ? "Human"
      : "System";

  // a11y: the marker is informational, not a control. The combined
  // aria-label gives screen readers the full context in one phrase rather
  // than relying on color or icon to convey origin.
  const ariaLabel = [
    `Patch from ${originLabel}`,
    `${patch.fileCount} ${fileLabel}, ${patch.hunkCount} ${hunkLabel}`,
    supersedes && "supersedes earlier change",
    failed && "from a failed turn",
  ]
    .filter(Boolean)
    .join("; ");

  return (
    <div
      role="status"
      aria-label={ariaLabel}
      className={cn(
        "mx-4 my-1 flex items-center gap-2 rounded-md border border-border-muted",
        "bg-background-subtle px-3 py-1.5 text-xs text-foreground-muted",
        "animate-fade-in-up",
      )}
    >
      <FileDiff
        className="h-3.5 w-3.5 shrink-0"
        style={{ color: badgeColor }}
        aria-hidden="true"
      />
      <span
        className="rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-foreground-on-emphasis"
        style={{ backgroundColor: badgeColor }}
      >
        {originLabel}
      </span>
      <span className="text-foreground">
        {patch.fileCount} {fileLabel}, {patch.hunkCount} {hunkLabel}
      </span>
      {supersedes && (
        <span className="flex items-center gap-1 text-foreground-subtle">
          <GitBranch className="h-3 w-3" aria-hidden="true" />
          supersedes earlier change
        </span>
      )}
      {failed && (
        <span
          className="flex items-center gap-1 text-warning"
          title="The agent reported an error mid-turn. Files captured here may be incomplete."
        >
          <AlertTriangle className="h-3 w-3" aria-hidden="true" />
          failed mid-turn
        </span>
      )}
    </div>
  );
}
