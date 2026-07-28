import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { MessageBubble } from "./ChatView";
import type { Message, User } from "@/types";

const author: User = {
  id: "u1",
  name: "Alice",
  email: "a@x.dev",
  avatar: "",
  status: "online" as User["status"],
};

function humanMessage(content: string): Message {
  return {
    id: "m1",
    sessionId: "s1",
    authorId: "u1",
    authorType: "human",
    content,
    createdAt: new Date(0).toISOString(),
    status: "sent",
  };
}

describe("MessageBubble", () => {
  it("renders markdown in a human message", () => {
    const { container } = render(
      <MessageBubble
        message={humanMessage("**bold** and\n\n- item one\n- item two")}
        author={author}
        showHeader
      />,
    );
    expect(container.querySelector("strong")).toHaveTextContent("bold");
    expect(container.querySelectorAll("ul > li")).toHaveLength(2);
    // No raw literal markdown left over.
    expect(container.textContent).not.toContain("**bold**");
  });

  it("renders markdown in a system notice (agent-typed, nil author)", () => {
    const msg: Message = {
      ...humanMessage("## Notice\n\nWorkspace restarted."),
      authorType: "agent",
      authorId: "00000000-0000-0000-0000-000000000000",
    };
    const { container } = render(
      <MessageBubble message={msg} author={undefined} showHeader />,
    );
    expect(container.querySelector("h2")).toHaveTextContent("Notice");
  });

  it("still renders expandable content untouched (R6)", () => {
    const msg: Message = {
      ...humanMessage("see diff"),
      expandableContent: [
        {
          type: "diff",
          title: "changes",
          summary: "1 file",
          content: "- old\n+ new",
        },
      ],
    };
    const { getByText } = render(
      <MessageBubble message={msg} author={author} showHeader />,
    );
    // The toggle button from expandableContent is still present.
    expect(getByText(/changes/)).toBeInTheDocument();
  });
});
