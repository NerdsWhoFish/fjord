import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { ConversationTimeline } from "./ConversationTimeline";
import { demoPR } from "./demo";

describe("PR conversation", () => {
  it("renders issue comments and empty review decisions without ID collisions", () => {
    const html = renderToStaticMarkup(
      <ConversationTimeline
        pr={demoPR}
        entries={[
          {
            id: 1,
            kind: "comment",
            body: "Regular PR conversation",
            author: "author",
            url: "",
            createdAt: "2026-11-01T12:00:00Z",
          },
          {
            id: 1,
            kind: "review",
            state: "APPROVED",
            body: "",
            author: "reviewer",
            url: "",
            createdAt: "2026-11-01T13:00:00Z",
          },
        ]}
      />,
    );
    expect(html).toContain("Regular PR conversation");
    expect(html).toContain("Approved");
    expect(html).toContain("Pull request description");
  });
  it("strips executable comment HTML and rejects non-GitHub links", () => {
    const html = renderToStaticMarkup(
      <ConversationTimeline
        pr={demoPR}
        entries={[
          {
            id: 2,
            kind: "comment",
            body: "<script>alert(1)</script>",
            author: "author",
            url: "javascript:alert(1)",
            createdAt: "",
          },
        ]}
      />,
    );
    expect(html).not.toContain("<script>");
    expect(html).not.toContain("alert(1)");
    expect(html).not.toContain("javascript:");
  });
});
