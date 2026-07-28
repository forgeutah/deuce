// Smoke test proving the jsdom + @testing-library render harness works.
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";

describe("render harness", () => {
  it("renders a component into jsdom and queries its text", () => {
    render(<div>harness ok</div>);
    expect(screen.getByText("harness ok")).toBeInTheDocument();
  });
});
