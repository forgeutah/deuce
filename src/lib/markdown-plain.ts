// toPlainText strips common markdown syntax and collapses whitespace to a
// single line. Used for compact, single-line surfaces (e.g. the terminal
// task-card summary) where a reply's block markdown would otherwise blow out
// the layout. Dependency-free — this is a display flattening, not a parser.
export function toPlainText(markdown: string): string {
  if (!markdown) return "";
  let s = markdown;
  // Images then links: ![alt](url) / [text](url) -> alt / text.
  s = s.replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1");
  s = s.replace(/\[([^\]]*)\]\([^)]*\)/g, "$1");
  // Fenced code delimiters (```lang / ```) -> drop the fence, keep the code text.
  s = s.replace(/```[^\n]*\n?/g, " ");
  // Remaining backticks (inline code).
  s = s.replace(/`+/g, "");
  // Leading block markers at line starts: headings, blockquotes, list bullets.
  s = s.replace(/^[ \t]*#{1,6}[ \t]+/gm, "");
  s = s.replace(/^[ \t]*>[ \t]?/gm, "");
  s = s.replace(/^[ \t]*(?:[-*+]|\d+\.)[ \t]+/gm, "");
  // Emphasis / strikethrough markers. Paired bold/strike and single asterisks
  // drop unconditionally; underscores only drop when they wrap a run as
  // emphasis, so snake_case identifiers (function_name) survive intact.
  s = s.replace(/\*\*|__|~~/g, "");
  s = s.replace(/\*/g, "");
  s = s.replace(/\b_([^_]+?)_\b/g, "$1");
  // Collapse all whitespace (incl. newlines) to single spaces.
  s = s.replace(/\s+/g, " ").trim();
  return s;
}
