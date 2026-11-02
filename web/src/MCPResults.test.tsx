import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { MCPResults } from "./MCPResults";
import { SettingsMenu } from "./SettingsMenu";
import { emptyDraft, ReviewPanel } from "./ReviewPanel";
import { demoPR } from "./demo";
import type { Selection } from "./types";

const selected: Selection = {
  path: "main.go",
  sha: "abcdef123",
  side: "RIGHT",
  line: 8,
  character: 0,
  code: "",
  word: "",
};
describe("MCP review surfaces", () => {
  it("shows combined assistant destinations with revision context and outline return", () => {
    const location = {
      path: "unchanged.go",
      line: 4,
      character: 0,
      endLine: 4,
      endCharacter: 5,
    };
    const html = renderToStaticMarkup(
      <MCPResults
        method="goTo"
        result={{
          locations: [],
          hover: "",
          definitions: [location],
          references: [location],
        }}
        selection={selected}
        onLocation={() => {}}
        onSuggest={async () => {}}
        onClearAnnotations={() => {}}
        onClose={() => {}}
      />,
    );
    expect(html).toContain("Head / abcdef1");
    expect(html).toContain('aria-label="Return to file outline"');
    expect(html.indexOf('aria-label="Definitions"')).toBeLessThan(
      html.indexOf('aria-label="References"'),
    );
    expect(html).toContain("unchanged.go:5");
  });
  it("exposes MCP connection settings without direct AI-provider configuration", () => {
    const html = renderToStaticMarkup(
      <SettingsMenu session={{ id: "session1", status: "connected" }} />,
    );
    expect(html).toContain("Ready for your AI client.");
    expect(html).toContain("Streamable HTTP");
    expect(html).toContain('value="/mcp"');
    expect(html).toContain("session1");
    expect(html).not.toContain("API key");
    expect(renderToStaticMarkup(<SettingsMenu />)).not.toContain(
      "MCP connection",
    );
  });
  it("labels assistant drafts and allows editing before submitting", () => {
    const html = renderToStaticMarkup(
      <ReviewPanel
        pr={demoPR}
        demo
        draft={{
          ...emptyDraft(),
          comments: [
            {
              id: "ai1",
              path: "main.go",
              line: 1,
              side: "RIGHT",
              body: "Check this case",
              source: "ai",
            },
          ],
        }}
        onDraft={() => {}}
        threads={[]}
        onReloadThreads={() => {}}
        onSubmit={async () => {}}
        onReply={async () => {}}
        onCloseComment={() => {}}
        open={false}
        onCloseReview={() => {}}
      />,
    );
    expect(html).toContain("Suggested by AI");
    expect(html).toContain("Edit draft");
    expect(html).toContain("Remove draft");
  });
});
