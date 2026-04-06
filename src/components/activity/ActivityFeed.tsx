import { File, GitCommit, CircleCheck, CircleX, Bot } from "lucide-react";
import { cn } from "@/lib/utils";
import type { ActivityItem } from "@/types";

const icons = {
  "file-change": File,
  "test-run": CircleCheck,
  commit: GitCommit,
  "agent-action": Bot,
};

function formatRelativeTime(timestamp: string): string {
  const diff = Date.now() - new Date(timestamp).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function ActivityFeed({ activities }: { activities: ActivityItem[] }) {
  if (activities.length === 0) {
    return (
      <p className="text-xs text-foreground-subtle text-center py-4">
        No activity yet
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-1">
      {activities.map((activity) => {
        const Icon = icons[activity.type] ?? Bot;
        return (
          <div
            key={activity.id}
            className="flex items-start gap-2 rounded-md px-1 py-1 text-xs"
          >
            <Icon
              className={cn(
                "mt-0.5 h-3 w-3 shrink-0",
                activity.type === "test-run" ? "text-success" : "text-foreground-subtle",
              )}
            />
            <div className="flex-1 min-w-0">
              <span className="text-foreground">{activity.description}</span>
              {activity.metadata?.additions && (
                <span className="ml-1 text-success">
                  +{activity.metadata.additions}
                </span>
              )}
              {activity.metadata?.deletions && (
                <span className="ml-1 text-danger">
                  -{activity.metadata.deletions}
                </span>
              )}
            </div>
            <span className="shrink-0 text-foreground-subtle">
              {formatRelativeTime(activity.timestamp)}
            </span>
          </div>
        );
      })}
    </div>
  );
}
