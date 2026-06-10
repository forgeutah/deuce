// AgentSettingsDialog — edits deuce's system prompt. There is one built-in
// agent; its prompt is a GLOBAL setting (it shapes deuce in every session,
// not just the one this dialog was opened from), and Pi applies it only at
// process launch — idle sessions pick an edit up on their next task, sessions
// mid-task on their next process launch.

import { useState, useEffect } from "react";
import { Loader2 } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { api, ApiError } from "@/lib/api";
import { DEUCE } from "@/lib/deuce";

export function AgentSettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [prompt, setPrompt] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (!open) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true);
    setError(null);
    setSaved(false);
    api
      .getAgentSettings()
      .then((settings) => setPrompt(settings.systemPrompt))
      .catch((err) =>
        setError(
          err instanceof ApiError ? err.message : "Failed to load settings.",
        ),
      )
      .finally(() => setLoading(false));
  }, [open]);

  async function save() {
    if (saving) return;
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      const settings = await api.updateAgentSettings(prompt);
      setPrompt(settings.systemPrompt);
      setSaved(true);
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Failed to save. Your edit was not stored — try again.",
      );
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <div
              className="flex h-6 w-6 items-center justify-center rounded text-xs font-semibold text-foreground-on-emphasis"
              style={{ backgroundColor: DEUCE.color }}
            >
              {DEUCE.name[0]}
            </div>
            {DEUCE.name} settings
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-2">
          <label
            htmlFor="deuce-system-prompt"
            className="text-xs font-medium text-foreground-muted"
          >
            System prompt
          </label>
          <textarea
            id="deuce-system-prompt"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            disabled={loading}
            rows={8}
            placeholder="Extra instructions for the agent (appended to the built-in guidance)…"
            className="resize-y rounded-md border border-border-muted bg-background-input px-3 py-2 text-sm text-foreground placeholder:text-foreground-subtle focus:border-accent focus:outline-none"
          />
          <p className="text-[11px] text-foreground-subtle">
            Applies to {DEUCE.name} in <b>all sessions</b>, not just this one.
            Takes effect on the agent's next process launch — sessions with a
            task in flight pick it up after their current run.
          </p>
          {error && (
            <p className="text-xs text-danger" role="alert">
              {error}
            </p>
          )}
          {saved && !error && (
            <p className="text-xs text-success" role="status">
              Saved.
            </p>
          )}
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Close
          </Button>
          <Button size="sm" onClick={save} disabled={loading || saving}>
            {saving && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            Save
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
