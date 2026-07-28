import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { Markdown } from "./markdown";

describe("Markdown", () => {
  it("renders headings, lists, bold, and inline code", () => {
    const { container } = render(
      <Markdown>{"## Heading\n\n- a\n- b\n\n**bold** and `x`"}</Markdown>,
    );
    expect(container.querySelector("h2")).toHaveTextContent("Heading");
    const items = container.querySelectorAll("ul > li");
    expect(items).toHaveLength(2);
    expect(container.querySelector("strong")).toHaveTextContent("bold");
    expect(container.querySelector("code")).toHaveTextContent("x");
  });

  it("renders GFM tables and task lists", () => {
    const { container } = render(
      <Markdown>
        {"| A | B |\n| - | - |\n| 1 | 2 |\n\n- [x] done\n- [ ] todo"}
      </Markdown>,
    );
    expect(container.querySelector("table")).toBeInTheDocument();
    const checkboxes = container.querySelectorAll(
      'input[type="checkbox"]',
    );
    expect(checkboxes).toHaveLength(2);
  });

  it("escapes raw HTML instead of rendering it (no injection)", () => {
    const { container } = render(
      <Markdown>{'text <img src=x onerror="alert(1)"> more'}</Markdown>,
    );
    // The raw <img> must not become a real element in the DOM.
    expect(container.querySelector("img")).toBeNull();
    expect(container).toHaveTextContent("onerror");
  });

  it("does not emit a javascript: link", () => {
    const { container } = render(
      <Markdown>{"[click](javascript:alert(1))"}</Markdown>,
    );
    const anchors = container.querySelectorAll("a");
    anchors.forEach((a) => {
      expect(a.getAttribute("href") ?? "").not.toContain("javascript:");
    });
    // The link text is still shown to the reader.
    expect(container).toHaveTextContent("click");
  });

  it("opens http links in a new tab with safe rel", () => {
    render(<Markdown>{"[docs](https://example.dev)"}</Markdown>);
    const link = screen.getByRole("link", { name: "docs" });
    expect(link).toHaveAttribute("href", "https://example.dev");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link.getAttribute("rel")).toContain("noopener");
    expect(link.getAttribute("rel")).toContain("noreferrer");
  });

  it("renders blank for empty or whitespace-only input without throwing", () => {
    const { container } = render(<Markdown>{"   "}</Markdown>);
    expect(container.querySelector(".md")).toBeInTheDocument();
    expect(container.textContent?.trim()).toBe("");
  });
});
