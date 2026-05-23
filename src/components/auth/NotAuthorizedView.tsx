import { useEffect, useRef } from "react";

interface NotAuthorizedViewProps {
  onRetry: () => void;
}

/**
 * Full-page security gate shown when the server returns 403 NOT_AUTHORIZED
 * from the first /api/me boot call. Replaces the entire app shell so the
 * user cannot interact with anything that would assume authentication.
 *
 * Static text only — does not render any header-derived data (email, name,
 * avatar). At this point the caller has been rejected by the auth gate and
 * rendering attacker-controlled strings would create an unnecessary attack
 * surface.
 */
export function NotAuthorizedView({ onRetry }: NotAuthorizedViewProps) {
  const headingRef = useRef<HTMLHeadingElement>(null);

  // Move keyboard focus to the heading on mount so screen-reader and
  // keyboard users land at the page summary instead of in the document
  // head. The heading carries tabIndex={-1} to make it focusable
  // programmatically without inserting it into the tab order.
  useEffect(() => {
    headingRef.current?.focus();
  }, []);

  return (
    <main
      role="main"
      className="dark flex h-screen w-screen items-center justify-center bg-background text-foreground"
    >
      <div className="flex max-w-md flex-col items-center gap-4 px-6 text-center">
        <h1
          ref={headingRef}
          tabIndex={-1}
          className="text-2xl font-semibold text-foreground outline-none"
        >
          Not Authorized
        </h1>
        <p className="text-sm text-foreground-muted">
          Your account doesn&apos;t have access to this Deuce workspace.
          Contact your system administrator to request access.
        </p>
        <button
          type="button"
          onClick={onRetry}
          className="mt-2 rounded-md bg-accent-emphasis px-4 py-1.5 text-sm text-foreground-on-emphasis hover:bg-accent"
        >
          Try again
        </button>
      </div>
    </main>
  );
}
