import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import type { User } from "@/types";
import { Loader2 } from "lucide-react";

interface WelcomeViewProps {
  email: string;
  onComplete: (user: User) => void;
}

const MAX_NAME_LEN = 100;

/**
 * Full-page welcome screen shown when /api/me returns a user without a
 * display name — i.e. the configured proxy didn't supply one (exe.dev,
 * any custom proxy that only forwards email). Submitting the form calls
 * PATCH /api/me, then the parent re-renders into the regular app shell.
 *
 * Replaces the entire app shell, same posture as NotAuthorizedView: the
 * user cannot interact with anything that would assume a populated name.
 */
export function WelcomeView({ email, onComplete }: WelcomeViewProps) {
  const [name, setName] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Focus the input on mount so the welcome flow needs zero clicks.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  async function handleSubmit(e: React.FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) {
      setError("Display name cannot be empty");
      return;
    }
    if (trimmed.length > MAX_NAME_LEN) {
      setError(`Display name too long (max ${MAX_NAME_LEN} characters)`);
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const updated = await api.updateMyName(trimmed);
      onComplete(updated);
    } catch (err: unknown) {
      const message =
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : "Failed to save display name";
      setError(message);
      setSubmitting(false);
    }
  }

  return (
    <main
      role="main"
      className="dark flex h-screen w-screen items-center justify-center bg-background text-foreground"
    >
      <form
        onSubmit={handleSubmit}
        className="flex w-full max-w-md flex-col gap-4 px-6"
      >
        <h1 className="text-2xl font-semibold text-foreground">
          Welcome to Deuce
        </h1>
        <p className="text-sm text-foreground-muted">
          Signed in as <span className="text-foreground">{email}</span>. Pick a
          display name your teammates will see — you can change it later.
        </p>
        <label className="flex flex-col gap-1.5">
          <span className="text-xs text-foreground-muted">Display name</span>
          <input
            ref={inputRef}
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            maxLength={MAX_NAME_LEN}
            disabled={submitting}
            autoComplete="name"
            className="rounded-md border border-border bg-canvas-subtle px-3 py-2 text-sm text-foreground outline-none focus:border-accent disabled:opacity-50"
            placeholder="e.g. Alex Park"
          />
        </label>
        {error && <p className="text-sm text-danger">{error}</p>}
        <button
          type="submit"
          disabled={submitting || name.trim().length === 0}
          className="mt-2 flex items-center justify-center gap-2 rounded-md bg-accent-emphasis px-4 py-1.5 text-sm text-foreground-on-emphasis hover:bg-accent disabled:opacity-50"
        >
          {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
          Continue
        </button>
      </form>
    </main>
  );
}
