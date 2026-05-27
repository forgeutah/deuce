import { useEffect, useState } from "react";
import { CheckCircle, Code, Copy } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ApiError, api } from "@/lib/api";

/**
 * SSHKeySetupModal — action-time SSH key capture.
 *
 * Opened programmatically by U14's "Open in VS Code" button when the backend
 * returns `412 NO_SSH_KEY` from `GET /api/sessions/:id/vscode-uri`. On submit
 * we POST the key, immediately retry the URI fetch, and navigate the browser
 * to the resulting `vscode://` URI. Errors keep the modal open with an inline
 * message so the user can correct and retry without losing what they typed.
 *
 * Focus restoration: shadcn `Dialog` returns focus to the element that opened
 * it via a `DialogTrigger`. This modal is opened programmatically (no
 * trigger) from U14's button, so we explicitly hand focus back to that button
 * in `onCloseAutoFocus`. The selector targets a stable `data-vscode-button`
 * attribute U14 must place on the trigger.
 */
export interface SSHKeySetupModalProps {
  sessionID: string;
  open: boolean;
  onClose: () => void;
}

type OSKind = "mac" | "linux" | "windows" | "unknown";

function detectOS(): OSKind {
  if (typeof navigator === "undefined") return "unknown";
  const ua = navigator.userAgent;
  if (ua.includes("Mac")) return "mac";
  if (ua.includes("Windows")) return "windows";
  if (ua.includes("Linux")) return "linux";
  return "unknown";
}

const MAC_LINUX_CMD = "cat ~/.ssh/id_ed25519.pub";
const WINDOWS_CMD = "type %USERPROFILE%\\.ssh\\id_ed25519.pub";
const KEYGEN_CMD = 'ssh-keygen -t ed25519 -C "you@deuce"';

export function SSHKeySetupModal({
  sessionID,
  open,
  onClose,
}: SSHKeySetupModalProps) {
  const [label, setLabel] = useState("");
  const [publicKey, setPublicKey] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset form when the modal closes so a future re-open starts clean.
  // The synchronous setState batch is intentional — React 18 batches these
  // into a single render and none are in this effect's dep list.
  useEffect(() => {
    if (!open) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setLabel("");
      setPublicKey("");
      setError(null);
      setSubmitting(false);
    }
  }, [open]);

  async function handleSubmit() {
    if (submitting) return; // no-op repeat-clicks during in-flight POST
    if (!label.trim() || !publicKey.trim()) {
      setError("Label and public key are both required.");
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      await api.createMySSHKey(label.trim(), publicKey.trim());
      // Retry the URI fetch now that the key is on file.
      const { uri } = await api.getSessionVSCodeURI(sessionID);
      // Navigate the browser to the vscode:// URI. The browser hands it off
      // to the OS handler; the page itself stays put. We still call onClose()
      // so the modal tears down cleanly if the user comes back.
      window.location.href = uri;
      onClose();
    } catch (err) {
      if (err instanceof ApiError) {
        // 503 from the URI fetch — proxy unavailable. Message is intentionally
        // distinct from the create-key error path.
        if (err.code === "SSH_UNAVAILABLE") {
          setError("Couldn't reach the VS Code service. Contact admin.");
        } else if (err.code === "KEY_ALREADY_EXISTS") {
          setError("That key is already on file under a different label.");
        } else {
          setError(err.message);
        }
      } else {
        setError("Something went wrong. Please try again.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent
        className="max-w-xl bg-background-subtle border-border-muted"
        onOpenAutoFocus={(e) => {
          // Focus the label input first — that's the user's entry point.
          // shadcn's `Input` is a function component that doesn't forward
          // refs to the underlying <input>, so we look it up by id rather
          // than holding a ref.
          e.preventDefault();
          const input = document.getElementById(
            "ssh-setup-label",
          ) as HTMLInputElement | null;
          input?.focus();
        }}
        onCloseAutoFocus={(e) => {
          // Modal was opened programmatically (no DialogTrigger), so Radix
          // can't restore focus on its own. Hand it back to U14's button if
          // we can find it; otherwise let the browser do its default.
          const trigger = document.querySelector<HTMLElement>(
            "[data-vscode-button]",
          );
          if (trigger) {
            e.preventDefault();
            trigger.focus();
          }
        }}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-foreground-emphasis">
            <Code className="h-5 w-5" />
            Add an SSH key to open in VS Code
          </DialogTitle>
          <DialogDescription className="text-foreground-muted">
            One-time setup. The key is used only by VS Code's Remote-SSH
            extension to connect to this session's workspace — Deuce never
            uses it to access anything else.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="ssh-setup-label"
              className="text-xs font-medium text-foreground-muted"
            >
              Label
            </label>
            <Input
              id="ssh-setup-label"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="e.g., laptop"
              disabled={submitting}
              className="h-8 bg-background border-border-muted text-sm"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label
              htmlFor="ssh-setup-public-key"
              className="text-xs font-medium text-foreground-muted"
            >
              Public key
            </label>
            <Textarea
              id="ssh-setup-public-key"
              value={publicKey}
              onChange={(e) => setPublicKey(e.target.value)}
              placeholder="ssh-ed25519 AAAAC3Nz... you@laptop"
              rows={4}
              disabled={submitting}
              className="bg-background border-border-muted text-sm font-mono resize-none"
            />
          </div>

          <HelperText />

          {error && (
            <div
              role="alert"
              className="rounded-md border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger"
            >
              {error}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            disabled={submitting}
            className="text-foreground-muted"
          >
            Cancel
          </Button>
          <Button
            size="sm"
            onClick={handleSubmit}
            disabled={submitting || !label.trim() || !publicKey.trim()}
            aria-busy={submitting}
            className="bg-accent-emphasis text-foreground-on-emphasis hover:bg-accent"
          >
            {submitting ? "Adding key…" : "Add and open VS Code"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * HelperText — OS-detected hint for grabbing the public key plus a fallback
 * `ssh-keygen` command. Each command line has a copy-to-clipboard button that
 * briefly flips to a checkmark and announces "Copied!" via an aria-live
 * region.
 *
 * Duplicated rather than shared with U12's SSHKeysDialog (~30 lines) per the
 * "three similar lines is better than a premature abstraction" rule. If U12
 * exports this later, swap to its export at merge time.
 */
function HelperText() {
  const os = detectOS();
  const showMacLinux = os === "mac" || os === "linux" || os === "unknown";
  const showWindows = os === "windows" || os === "unknown";

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border-muted bg-background px-3 py-2.5 text-xs text-foreground-muted">
      <div className="font-medium text-foreground">Where do I get this?</div>
      {showMacLinux && (
        <CommandLine label="macOS / Linux" command={MAC_LINUX_CMD} />
      )}
      {showWindows && <CommandLine label="Windows" command={WINDOWS_CMD} />}
      <div className="pt-1 text-foreground-subtle">
        Don't have one?{" "}
        <CommandLine inline label="" command={KEYGEN_CMD} />
      </div>
    </div>
  );
}

function CommandLine({
  label,
  command,
  inline = false,
}: {
  label: string;
  command: string;
  inline?: boolean;
}) {
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard write rejected (permissions, insecure context). Silently
      // ignore — the user can still select the command text manually.
    }
  }

  return (
    <div
      className={
        inline
          ? "inline-flex items-center gap-2 align-middle"
          : "flex items-center gap-2"
      }
    >
      {label && (
        <span className="shrink-0 text-foreground-subtle">{label}:</span>
      )}
      <code className="flex-1 truncate rounded bg-background-subtle px-1.5 py-0.5 font-mono text-foreground">
        {command}
      </code>
      <button
        type="button"
        onClick={copy}
        aria-label={copied ? "Copied" : `Copy ${command}`}
        className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-foreground-muted hover:bg-background-subtle hover:text-foreground focus:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {copied ? (
          <CheckCircle className="h-3.5 w-3.5 text-success" />
        ) : (
          <Copy className="h-3.5 w-3.5" />
        )}
      </button>
      {/* aria-live announces "Copied!" to assistive tech without stealing
          focus. The visible state lives in the icon swap above. */}
      <span aria-live="polite" className="sr-only">
        {copied ? "Copied!" : ""}
      </span>
    </div>
  );
}
