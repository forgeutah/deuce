import { useEffect, useState } from "react";
import {
  CheckCircle,
  Copy,
  Key,
  Plus,
  Trash2,
  X,
} from "lucide-react";
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
import { api, ApiError } from "@/lib/api";
import type { SSHKey } from "@/types";

// Map server-supplied ApiError.code values for the create endpoint to
// inline messages the user can act on. Codes match the backend contract
// defined in U8 of the VS Code SSH proxy plan.
const ADD_ERROR_MESSAGES: Record<string, string> = {
  INVALID_KEY_FORMAT: "That doesn't look like a valid SSH public key.",
  KEY_TOO_LONG: "Key too long (max 8KB).",
  KEY_ALREADY_EXISTS: "You've already added this key.",
};

const SUCCESS_BANNER_MS = 5000;
const COPIED_LABEL_MS = 2000;

type OS = "mac" | "linux" | "windows" | "unknown";

function detectOS(): OS {
  if (typeof navigator === "undefined") return "unknown";
  const ua = navigator.userAgent;
  if (/Mac/i.test(ua)) return "mac";
  if (/Linux/i.test(ua)) return "linux";
  if (/Windows/i.test(ua)) return "windows";
  return "unknown";
}

const UNIX_CAT_CMD = "cat ~/.ssh/id_ed25519.pub";
const WINDOWS_TYPE_CMD = "type %USERPROFILE%\\.ssh\\id_ed25519.pub";

// shortFingerprint trims the SHA256:… prefix down to the first 12 chars of
// the digest plus an ellipsis. Long fingerprints don't fit inside a row
// and the short form is enough to disambiguate visually.
function shortFingerprint(fp: string): string {
  if (!fp) return "";
  const colonIdx = fp.indexOf(":");
  if (colonIdx === -1) return fp.slice(0, 16) + "…";
  const prefix = fp.slice(0, colonIdx + 1);
  const digest = fp.slice(colonIdx + 1);
  if (digest.length <= 16) return fp;
  return `${prefix}${digest.slice(0, 12)}…`;
}

function formatDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString(undefined, {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
  } catch {
    return iso;
  }
}

export function SSHKeysDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [keys, setKeys] = useState<SSHKey[]>([]);
  const [loading, setLoading] = useState(false);
  const [deletingKeyId, setDeletingKeyId] = useState<string | null>(null);
  const [recentlyAdded, setRecentlyAdded] = useState<SSHKey | null>(null);

  // Add-form state
  const [newLabel, setNewLabel] = useState("");
  const [newPublicKey, setNewPublicKey] = useState("");
  const [addError, setAddError] = useState<string | null>(null);

  // Copy-button feedback
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  useEffect(() => {
    if (open) loadKeys();
  }, [open]);

  // Auto-dismiss the success banner after R15 confirmation window.
  useEffect(() => {
    if (!recentlyAdded) return;
    const t = setTimeout(() => setRecentlyAdded(null), SUCCESS_BANNER_MS);
    return () => clearTimeout(t);
  }, [recentlyAdded]);

  // Auto-clear copy-button feedback so the button returns to its default
  // "Copy" label after the user has had time to register the change.
  useEffect(() => {
    if (!copiedKey) return;
    const t = setTimeout(() => setCopiedKey(null), COPIED_LABEL_MS);
    return () => clearTimeout(t);
  }, [copiedKey]);

  // Reset transient state on close so reopening the dialog starts fresh.
  useEffect(() => {
    if (!open) {
      setDeletingKeyId(null);
      setRecentlyAdded(null);
      setAddError(null);
      setCopiedKey(null);
    }
  }, [open]);

  async function loadKeys() {
    try {
      const data = await api.listMySSHKeys();
      setKeys(data);
    } catch (err) {
      // Mirror AgentSettingsDialog's silent-swallow pattern — surface a
      // dedicated load-error UI is intentionally out of scope for U12.
      console.error("Failed to load SSH keys:", err);
    }
  }

  async function addKey() {
    const label = newLabel.trim();
    const publicKey = newPublicKey.trim();
    if (!label || !publicKey) return;
    setLoading(true);
    setAddError(null);
    try {
      const created = await api.createMySSHKey(label, publicKey);
      setKeys((prev) => [created, ...prev]);
      setRecentlyAdded(created);
      setNewLabel("");
      setNewPublicKey("");
    } catch (err) {
      if (err instanceof ApiError) {
        setAddError(
          ADD_ERROR_MESSAGES[err.code] ?? err.message ?? "Couldn't add key.",
        );
      } else {
        setAddError("Couldn't add key.");
      }
    } finally {
      setLoading(false);
    }
  }

  async function deleteKey(id: string) {
    try {
      await api.deleteMySSHKey(id);
      setKeys((prev) => prev.filter((k) => k.id !== id));
      setDeletingKeyId(null);
    } catch (err) {
      console.error("Failed to delete SSH key:", err);
    }
  }

  const os = detectOS();
  const showUnix = os === "mac" || os === "linux" || os === "unknown";
  const showWindows = os === "windows" || os === "unknown";

  async function copyCommand(cmd: string, slot: string) {
    try {
      await navigator.clipboard.writeText(cmd);
      setCopiedKey(slot);
    } catch (err) {
      console.error("Failed to copy command:", err);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl bg-background-subtle border-border-muted">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-foreground-emphasis">
            <Key className="h-5 w-5" />
            SSH Keys
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-col gap-3">
          <p className="text-sm text-foreground-muted">
            Add a public key to open Deuce sessions in VS Code over SSH.
          </p>

          {recentlyAdded && (
            <div
              role="status"
              aria-live="polite"
              className="flex items-start gap-2 rounded-md border border-success/40 bg-success-muted/30 px-3 py-2 text-sm text-foreground"
            >
              <CheckCircle className="mt-0.5 h-4 w-4 shrink-0 text-success" />
              <div className="min-w-0 flex-1">
                <span className="font-medium text-foreground-emphasis">
                  Added:
                </span>{" "}
                <span className="font-medium">{recentlyAdded.label}</span>{" "}
                <span className="text-foreground-muted">
                  ({shortFingerprint(recentlyAdded.fingerprint)})
                </span>
              </div>
            </div>
          )}

          <Separator className="bg-border-muted" />

          {/* Existing keys */}
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <p className="text-sm text-foreground-muted">
                {keys.length} key{keys.length !== 1 ? "s" : ""} on file
              </p>
            </div>

            <ScrollArea className="max-h-[260px]">
              {keys.length === 0 ? (
                <p className="px-2 py-6 text-center text-xs text-foreground-subtle">
                  No SSH keys yet. Add one below to open sessions in VS Code.
                </p>
              ) : (
                <div className="flex flex-col gap-2">
                  {keys.map((key) => (
                    <div
                      key={key.id}
                      className="flex items-center gap-3 rounded-md border border-border-muted bg-background p-3"
                    >
                      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-background-subtle text-foreground-muted">
                        <Key className="h-4 w-4" />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate text-sm font-medium text-foreground-emphasis">
                            {key.label}
                          </span>
                        </div>
                        <p className="truncate font-mono text-xs text-foreground-muted">
                          {shortFingerprint(key.fingerprint)}
                        </p>
                        <p className="text-xs text-foreground-subtle">
                          Added {formatDate(key.createdAt)}
                          {key.lastUsedAt && (
                            <> · Last used {formatDate(key.lastUsedAt)}</>
                          )}
                        </p>
                      </div>
                      <div className="flex items-center gap-1">
                        {deletingKeyId === key.id ? (
                          <div className="flex items-center gap-1">
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label="Confirm delete"
                              className="h-7 w-7 text-danger hover:bg-danger/10 hover:text-danger"
                              onClick={() => deleteKey(key.id)}
                            >
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label="Cancel delete"
                              className="h-7 w-7 text-foreground-muted"
                              onClick={() => setDeletingKeyId(null)}
                            >
                              <X className="h-3.5 w-3.5" />
                            </Button>
                          </div>
                        ) : (
                          <Button
                            variant="ghost"
                            size="icon"
                            aria-label={`Delete ${key.label}`}
                            className="h-7 w-7 text-foreground-muted hover:text-danger"
                            onClick={() => setDeletingKeyId(key.id)}
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </ScrollArea>
          </div>

          <Separator className="bg-border-muted" />

          {/* Add-key form */}
          <div className="flex flex-col gap-3">
            <h3 className="text-sm font-medium text-foreground-emphasis">
              Add a new key
            </h3>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground-muted">
                Label *
              </label>
              <Input
                value={newLabel}
                onChange={(e) => setNewLabel(e.target.value)}
                placeholder="e.g., Laptop"
                className="h-8 bg-background border-border-muted text-sm"
                disabled={loading}
              />
            </div>

            <div className="flex flex-col gap-1.5">
              <label className="text-xs font-medium text-foreground-muted">
                Public key *
              </label>
              <Textarea
                value={newPublicKey}
                onChange={(e) => setNewPublicKey(e.target.value)}
                placeholder="ssh-ed25519 AAAA… you@host"
                rows={4}
                className="bg-background border-border-muted text-sm font-mono resize-none"
                disabled={loading}
                spellCheck={false}
              />
            </div>

            {/* OS-detected helper text + copy button */}
            <div className="flex flex-col gap-2 rounded-md border border-border-muted bg-background p-3">
              <p className="text-xs text-foreground-muted">
                Find your public key:
              </p>
              {showUnix && (
                <HelperCommandRow
                  cmd={UNIX_CAT_CMD}
                  copied={copiedKey === "unix"}
                  onCopy={() => copyCommand(UNIX_CAT_CMD, "unix")}
                />
              )}
              {showWindows && (
                <HelperCommandRow
                  cmd={WINDOWS_TYPE_CMD}
                  copied={copiedKey === "windows"}
                  onCopy={() => copyCommand(WINDOWS_TYPE_CMD, "windows")}
                />
              )}
              <p className="text-xs text-foreground-subtle">
                Don't have one?{" "}
                <code className="rounded bg-background-subtle px-1 py-0.5 font-mono text-foreground-muted">
                  ssh-keygen -t ed25519 -C "you@deuce"
                </code>
              </p>
            </div>

            {addError && (
              <p
                role="alert"
                className="text-xs text-danger"
              >
                {addError}
              </p>
            )}

            <div className="flex items-center justify-end gap-2">
              <Button
                size="sm"
                onClick={addKey}
                disabled={loading || !newLabel.trim() || !newPublicKey.trim()}
                className="gap-1.5 bg-accent-emphasis text-foreground-on-emphasis hover:bg-accent"
              >
                <Plus className="h-3.5 w-3.5" />
                {loading ? "Adding…" : "Add key"}
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function HelperCommandRow({
  cmd,
  copied,
  onCopy,
}: {
  cmd: string;
  copied: boolean;
  onCopy: () => void;
}) {
  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate rounded bg-background-subtle px-2 py-1 font-mono text-xs text-foreground-muted">
        {cmd}
      </code>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={onCopy}
        aria-label={`Copy command: ${cmd}`}
        className="h-7 gap-1.5 px-2 text-xs text-foreground-muted hover:text-foreground"
      >
        {copied ? (
          <>
            <CheckCircle className="h-3.5 w-3.5 text-success" />
            Copied!
          </>
        ) : (
          <>
            <Copy className="h-3.5 w-3.5" />
            Copy
          </>
        )}
      </Button>
      {/* Screen-reader-only live region for copy confirmation. */}
      <span className="sr-only" aria-live="polite">
        {copied ? "Command copied to clipboard" : ""}
      </span>
    </div>
  );
}
