import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { Markdown } from "./markdown";

// Prism's async light build registers/highlights on mount; findBy* waits for
// the resulting token spans to appear.
describe("Markdown fenced code", () => {
  it("highlights a language-tagged block", async () => {
    const { container, findByText } = render(
      <Markdown>{"```ts\nconst x: number = 1\n```"}</Markdown>,
    );
    // Highlighter emits token <span>s; wait for one carrying the keyword.
    await findByText("const");
    expect(container.querySelectorAll("span").length).toBeGreaterThan(1);
    expect(container).toHaveTextContent("const x: number = 1");
  });

  it("renders a no-language fenced block as a plain styled block", () => {
    const { container } = render(
      <Markdown>{"```\njust text\nmore\n```"}</Markdown>,
    );
    const pre = container.querySelector("pre.md-code--plain");
    expect(pre).toBeInTheDocument();
    expect(pre).toHaveTextContent("just text");
  });

  it("falls back gracefully for an unregistered language", () => {
    const { container } = render(
      <Markdown>{"```brainfuck\n+[----->+++<]>++.\n```"}</Markdown>,
    );
    const pre = container.querySelector("pre.md-code--plain");
    expect(pre).toBeInTheDocument();
    expect(pre).toHaveTextContent("+[----->+++<]>++.");
  });

  it("keeps inline code as a simple inline <code>, not a block", () => {
    const { container } = render(<Markdown>{"use `npm test` now"}</Markdown>);
    const code = container.querySelector("code");
    expect(code).toHaveTextContent("npm test");
    expect(container.querySelector("pre")).toBeNull();
  });
});
