// markdown-code — the `code`/`pre` renderer overrides for <Markdown>. Fenced
// blocks with a supported language are syntax-highlighted with Prism (async
// light build + a curated language set to keep bundle weight down); fenced
// blocks with no/unknown language fall back to a styled but unhighlighted
// block; inline `code` stays a simple styled span handled by prose CSS.

import { PrismAsyncLight as SyntaxHighlighter } from "react-syntax-highlighter";
import oneDark from "react-syntax-highlighter/dist/esm/styles/prism/one-dark";
import bash from "react-syntax-highlighter/dist/esm/languages/prism/bash";
import diff from "react-syntax-highlighter/dist/esm/languages/prism/diff";
import go from "react-syntax-highlighter/dist/esm/languages/prism/go";
import javascript from "react-syntax-highlighter/dist/esm/languages/prism/javascript";
import jsx from "react-syntax-highlighter/dist/esm/languages/prism/jsx";
import json from "react-syntax-highlighter/dist/esm/languages/prism/json";
import markdown from "react-syntax-highlighter/dist/esm/languages/prism/markdown";
import sql from "react-syntax-highlighter/dist/esm/languages/prism/sql";
import tsx from "react-syntax-highlighter/dist/esm/languages/prism/tsx";
import typescript from "react-syntax-highlighter/dist/esm/languages/prism/typescript";
import yaml from "react-syntax-highlighter/dist/esm/languages/prism/yaml";
import type { Components } from "react-markdown";

// Curated language set. Aliases map onto the registered canonical names so
// common fence tags (```js, ```sh, ```yml) resolve.
const LANGUAGES: Record<string, Parameters<typeof SyntaxHighlighter.registerLanguage>[1]> = {
  bash,
  diff,
  go,
  javascript,
  jsx,
  json,
  markdown,
  sql,
  tsx,
  typescript,
  yaml,
};

for (const [name, lang] of Object.entries(LANGUAGES)) {
  SyntaxHighlighter.registerLanguage(name, lang);
}

const ALIASES: Record<string, string> = {
  js: "javascript",
  ts: "typescript",
  sh: "bash",
  shell: "bash",
  yml: "yaml",
  md: "markdown",
  golang: "go",
};

function resolveLang(raw: string | undefined): string | undefined {
  if (!raw) return undefined;
  const lower = raw.toLowerCase();
  const canonical = ALIASES[lower] ?? lower;
  return canonical in LANGUAGES ? canonical : undefined;
}

export const codeComponents: Components = {
  // Unwrap the markdown-emitted <pre> so a highlighted block (which brings its
  // own container) isn't nested inside a second <pre>.
  pre({ children }) {
    return <>{children}</>;
  },
  code({ node: _node, className, children, ...props }) {
    const text = String(children ?? "");
    const match = /language-(\w+)/.exec(className ?? "");
    // A fenced block is either language-tagged or multiline; everything else is
    // inline code and stays a simple <code> styled by prose CSS.
    const isBlock = match !== null || text.includes("\n");
    if (!isBlock) {
      return (
        <code className={className} {...props}>
          {children}
        </code>
      );
    }

    const lang = resolveLang(match?.[1]);
    const code = text.replace(/\n$/, "");
    if (!lang) {
      // Unknown/absent language — styled but unhighlighted block.
      return (
        <pre className="md-code md-code--plain">
          <code>{code}</code>
        </pre>
      );
    }
    return (
      <SyntaxHighlighter
        language={lang}
        style={oneDark}
        PreTag="div"
        className="md-code"
        customStyle={{ margin: 0, background: "transparent" }}
      >
        {code}
      </SyntaxHighlighter>
    );
  },
};
