import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { AgentThreadDrawer } from "./AgentThreadDrawer";
import type { AgentTask } from "@/types";

function task(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "t1",
    sessionId: "s1",
    prompt: "@deuce explain this repo",
    state: "done",
    seq: 1,
    actions: [],
    ...overrides,
  };
}

function renderDrawer(tasks: AgentTask[]) {
  return render(
    <AgentThreadDrawer
      sessionId="s1"
      tasks={tasks}
      queuePositions={{}}
      lookupUser={() => ({ name: "Alice", avatar: "" })}
      onClose={() => {}}
      onSend={() => {}}
    />,
  );
}

describe("AgentThreadDrawer reply rendering", () => {
  it("renders a markdown reply as formatted markup", () => {
    const { container } = renderDrawer([
      task({ reply: "## Summary\n\n- one\n- two\n\n**done**" }),
    ]);
    expect(container.querySelector("h2")).toHaveTextContent("Summary");
    expect(container.querySelectorAll("ul > li")).toHaveLength(2);
    expect(container.querySelector("strong")).toHaveTextContent("done");
  });

  it("shows the fallback string when a failed task has no reply", () => {
    const { container } = renderDrawer([
      task({ state: "failed", reply: undefined }),
    ]);
    expect(container).toHaveTextContent("Run failed.");
    expect(container.querySelector(".bd h2")).toBeNull();
  });

  it("shows the cancelled fallback when a cancelled task has no reply", () => {
    const { container } = renderDrawer([
      task({ state: "cancelled", reply: undefined }),
    ]);
    expect(container).toHaveTextContent("Run cancelled.");
  });
});
