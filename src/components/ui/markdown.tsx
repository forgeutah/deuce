// Markdown — the single shared prose renderer for all message text (agent
// replies, system notices, human chat messages). Wraps react-markdown with
// remark-gfm (tables, task lists, strikethrough, autolinks).
//
// Safety: raw HTML is NOT rendered — react-markdown escapes it by default (we
// deliberately do not add rehype-raw), and its default urlTransform strips
// dangerous URL protocols. On top of that, the `a` override enforces a
// protocol allowlist and opens links in a new tab with safe rel attributes.
// All message content — model-generated and human-typed — is treated as
// untrusted for rendering.

import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import remarkBreaks from "remark-breaks";
import { cn } from "@/lib/utils";
import { codeComponents } from "./markdown-code";

// Only these protocols produce a real anchor. Relative links (no protocol)
// resolve against a dummy base to https and are allowed; javascript:, data:,
// vbscript:, etc. fall through to a plain <span>.
const SAFE_LINK_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);

function isSafeHref(href: string | undefined): href is string {
  if (!href) return false;
  try {
    const url = new URL(href, "https://deuce.local/");
    return SAFE_LINK_PROTOCOLS.has(url.protocol);
  } catch {
    return false;
  }
}

const components: Components = {
  ...codeComponents,
  a({ node: _node, href, children, ...props }) {
    if (!isSafeHref(href)) {
      // Unsafe or unparseable protocol — render the link text, no anchor.
      return <span {...props}>{children}</span>;
    }
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer nofollow"
        {...props}
      >
        {children}
      </a>
    );
  },
};

export function Markdown({
  children,
  className,
}: {
  children: string;
  className?: string;
}) {
  return (
    <div className={cn("md", className)}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkBreaks]}
        components={components}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}
