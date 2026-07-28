import { describe, it, expect } from "vitest";
import { toPlainText } from "./markdown-plain";

describe("toPlainText", () => {
  it("flattens a heading + list to one line", () => {
    expect(toPlainText("## Title\n\n- one\n- two")).toBe("Title one two");
  });

  it("strips emphasis and inline code markers", () => {
    expect(toPlainText("See **bold** and `code` here")).toBe(
      "See bold and code here",
    );
  });

  it("reduces a link to its text", () => {
    expect(toPlainText("[docs](https://x.dev)")).toBe("docs");
  });

  it("reduces an image to its alt text", () => {
    expect(toPlainText("![a diagram](/img.png) after")).toBe("a diagram after");
  });

  it("collapses a fenced code block to its inner text with no backticks", () => {
    const out = toPlainText("Run:\n```ts\nconst x = 1\n```");
    expect(out).toBe("Run: const x = 1");
    expect(out).not.toContain("`");
  });

  it("strips ordered-list markers", () => {
    expect(toPlainText("1. first\n2. second")).toBe("first second");
  });

  it("returns empty string for empty or whitespace-only input", () => {
    expect(toPlainText("")).toBe("");
    expect(toPlainText("   \n\t  ")).toBe("");
  });

  it("passes plain text through modulo whitespace collapse", () => {
    expect(toPlainText("just  some\n\ntext")).toBe("just some text");
  });
});
