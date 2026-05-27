import { useState, useEffect } from "react";
import { Bot, Plus, Pencil, Trash2, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { api } from "@/lib/api";
import type { Agent } from "@/types";

// AgentDraft mirrors Agent but drops the status field, which is server-managed
// (idle/working/etc.) and never edited from this dialog.
type AgentDraft = Omit<Agent, "status">;

const MODEL_OPTIONS = [
  "claude-opus-4-6",
  "claude-sonnet-4-6",
  "claude-haiku-4-5",
];

export function AgentSettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [editing, setEditing] = useState<AgentDraft | null>(null);
  const [isNew, setIsNew] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (open) loadAgents();
  }, [open]);

  async function loadAgents() {
    try {
      const data = await api.listAgents();
      setAgents(data);
    } catch (err) {
      console.error("Failed to load agents:", err);
    }
  }

  function startCreate() {
    setEditing({
      id: "",
      name: "",
      role: "",
      color: "",
      colorMuted: "",
      provider: "Anthropic",
      model: "claude-sonnet-4-6",
      description: "",
      systemPrompt: "",
    });
    setIsNew(true);
  }

  function startEdit(agent: Agent) {
    setEditing({ ...agent });
    setIsNew(false);
  }

  function cancelEdit() {
    setEditing(null);
    setIsNew(false);
  }

  async function saveAgent() {
    if (!editing || !editing.name.trim()) return;
    setLoading(true);
    try {
      if (isNew) {
        await api.createAgent({
          name: editing.name,
          role: editing.role,
          provider: editing.provider,
          model: editing.model,
          description: editing.description,
          systemPrompt: editing.systemPrompt,
        });
      } else {
        await api.updateAgent(editing.id, {
          name: editing.name,
          role: editing.role,
          provider: editing.provider,
          model: editing.model,
          description: editing.description,
          systemPrompt: editing.systemPrompt,
        });
      }
      await loadAgents();
      setEditing(null);
      setIsNew(false);
    } catch (err) {
      console.error("Failed to save agent:", err);
    } finally {
      setLoading(false);
    }
  }

  async function deleteAgent(id: string) {
    try {
      await api.deleteAgent(id);
      await loadAgents();
      setDeleting(null);
    } catch (err: unknown) {
      console.error("Failed to delete agent:", err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl bg-background-subtle border-border-muted">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-foreground-emphasis">
            <Bot className="h-5 w-5" />
            Agent Settings
          </DialogTitle>
        </DialogHeader>

        {editing ? (
          <AgentForm
            agent={editing}
            isNew={isNew}
            loading={loading}
            onChange={setEditing}
            onSave={saveAgent}
            onCancel={cancelEdit}
          />
        ) : (
          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between">
              <p className="text-sm text-foreground-muted">
                {agents.length} agent{agents.length !== 1 ? "s" : ""} configured
              </p>
              <Button
                size="sm"
                onClick={startCreate}
                className="gap-1.5 bg-accent-emphasis text-foreground-on-emphasis hover:bg-accent"
              >
                <Plus className="h-3.5 w-3.5" />
                Add Agent
              </Button>
            </div>

            <Separator className="bg-border-muted" />

            <ScrollArea className="max-h-[400px]">
              <div className="flex flex-col gap-2">
                {agents.map((agent) => (
                  <div
                    key={agent.id}
                    className="flex items-center gap-3 rounded-md border border-border-muted bg-background p-3"
                  >
                    <div
                      className="h-8 w-8 shrink-0 rounded-md flex items-center justify-center text-xs font-bold text-white"
                      style={{ backgroundColor: agent.color }}
                    >
                      {agent.name.charAt(0).toUpperCase()}
                    </div>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm text-foreground-emphasis">
                          {agent.name}
                        </span>
                        {agent.role && (
                          <span className="text-xs text-foreground-subtle px-1.5 py-0.5 rounded bg-background-subtle">
                            {agent.role}
                          </span>
                        )}
                      </div>
                      <p className="text-xs text-foreground-muted truncate">
                        {agent.model} {agent.description && `· ${agent.description}`}
                      </p>
                    </div>
                    <div className="flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 text-foreground-muted hover:text-foreground"
                        onClick={() => startEdit(agent)}
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      {deleting === agent.id ? (
                        <div className="flex items-center gap-1">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 text-danger hover:text-danger hover:bg-danger/10"
                            onClick={() => deleteAgent(agent.id)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 text-foreground-muted"
                            onClick={() => setDeleting(null)}
                          >
                            <X className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      ) : (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-7 w-7 text-foreground-muted hover:text-danger"
                          onClick={() => setDeleting(agent.id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </ScrollArea>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function AgentForm({
  agent,
  isNew,
  loading,
  onChange,
  onSave,
  onCancel,
}: {
  agent: AgentDraft;
  isNew: boolean;
  loading: boolean;
  onChange: (agent: AgentDraft) => void;
  onSave: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground-muted">Name *</label>
          <Input
            value={agent.name}
            onChange={(e) => onChange({ ...agent, name: e.target.value })}
            placeholder="e.g., Coder"
            className="h-8 bg-background border-border-muted text-sm"
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground-muted">Role</label>
          <Input
            value={agent.role}
            onChange={(e) => onChange({ ...agent, role: e.target.value })}
            placeholder="e.g., coder, reviewer"
            className="h-8 bg-background border-border-muted text-sm"
          />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground-muted">Model</label>
          <select
            value={agent.model}
            onChange={(e) => onChange({ ...agent, model: e.target.value })}
            className={cn(
              "h-8 w-full rounded-md border border-border-muted bg-background px-2 text-sm text-foreground",
              "focus:border-accent focus:outline-none focus:ring-1 focus:ring-accent",
            )}
          >
            {MODEL_OPTIONS.map((m) => (
              <option key={m} value={m}>
                {m}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1.5">
          <label className="text-xs font-medium text-foreground-muted">Description</label>
          <Input
            value={agent.description}
            onChange={(e) => onChange({ ...agent, description: e.target.value })}
            placeholder="Brief description"
            className="h-8 bg-background border-border-muted text-sm"
          />
        </div>
      </div>

      <div className="flex flex-col gap-1.5">
        <label className="text-xs font-medium text-foreground-muted">System Prompt</label>
        <Textarea
          value={agent.systemPrompt}
          onChange={(e) => onChange({ ...agent, systemPrompt: e.target.value })}
          placeholder="Instructions for what this agent does, its personality, constraints..."
          rows={6}
          className="bg-background border-border-muted text-sm resize-none"
        />
      </div>

      <div className="flex items-center justify-end gap-2 pt-2">
        <Button
          variant="ghost"
          size="sm"
          onClick={onCancel}
          className="text-foreground-muted"
        >
          Cancel
        </Button>
        <Button
          size="sm"
          onClick={onSave}
          disabled={!agent.name.trim() || loading}
          className="bg-accent-emphasis text-foreground-on-emphasis hover:bg-accent"
        >
          {loading ? "Saving..." : isNew ? "Create Agent" : "Save Changes"}
        </Button>
      </div>
    </div>
  );
}
