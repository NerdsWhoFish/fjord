import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { FeatureResults } from "./FeatureResults";
import type {
  CodeLocation,
  Method,
  NavigationResult,
  Selection,
} from "./types";

const location: CodeLocation = {
  path: "main.go",
  line: 3,
  character: 0,
  endLine: 3,
  endCharacter: 5,
};
const selection: Selection = {
  path: "main.go",
  sha: "head",
  side: "RIGHT",
  line: 4,
  character: 0,
  word: "value",
  code: "value",
};
function render(method: Method, result: NavigationResult) {
  const onSuggest = vi.fn(async () => {});
  return {
    onSuggest,
    html: renderToStaticMarkup(
      <FeatureResults
        method={method}
        result={result}
        selection={selection}
        onLocation={() => {}}
        onSuggest={onSuggest}
        onClearAnnotations={() => {}}
      />,
    ),
  };
}

describe("contextual language-server results", () => {
  it("shows definitions before references with both groups always labeled", () => {
    const { html } = render("goTo", {
      locations: [],
      hover: "",
      definitions: [location],
      references: [],
    });
    expect(html.indexOf('aria-label="Definitions"')).toBeLessThan(
      html.indexOf('aria-label="References"'),
    );
    expect(html).toContain("main.go:4");
    expect(html).toContain("No references returned.");
  });

  it("renders diagnostic severity and nested outline destinations as text", () => {
    const { html } = render("diagnostics", {
      locations: [],
      hover: "",
      items: [
        {
          label: "<unresolved>",
          kind: "warning",
          location,
          children: [{ label: "Child", location: { ...location, line: 7 } }],
        },
      ],
    });
    expect(html).toContain("warning");
    expect(html).toContain("&lt;unresolved&gt;");
    expect(html).toContain("main.go:8");
  });

  it("previews code action edits and requires explicit suggestion action", () => {
    const { html, onSuggest } = render("codeActions", {
      locations: [],
      hover: "",
      actions: [
        {
          title: "Fix value",
          edits: [{ location, oldText: "value", newText: "other" }],
        },
      ],
    });
    expect(html).toContain("Fix value");
    expect(html).toContain("Add review suggestions");
    expect(html).toContain("context-edit-before");
    expect(html).toContain("context-edit-after");
    expect(onSuggest).not.toHaveBeenCalled();
  });

  it("keeps rename previews read-only even if actions appear in the response", () => {
    const { html, onSuggest } = render("rename", {
      locations: [],
      hover: "",
      edits: [{ location, newText: "renamed" }],
      actions: [
        {
          title: "Unexpected action",
          edits: [{ location, newText: "renamed" }],
        },
      ],
    });
    expect(html).toContain("Read-only rename preview");
    expect(html).not.toContain("Add review suggestions");
    expect(onSuggest).not.toHaveBeenCalled();
  });

  it("explains disabled code actions and applied inline annotation counts", () => {
    const disabled = render("codeActions", {
      locations: [],
      hover: "",
      actions: [
        {
          title: "Unavailable",
          disabledReason: "Needs dependencies",
          edits: [{ location, newText: "next" }],
        },
      ],
    }).html;
    expect(disabled).toContain("Needs dependencies");
    expect(disabled).toContain('disabled=""');
    expect(
      render("documentHighlights", {
        locations: [],
        hover: "",
        highlights: [{ location }],
      }).html,
    ).toContain("1 occurrence range highlighted");
    expect(
      render("inlayHints", {
        locations: [],
        hover: "",
        hints: [{ location, label: ": int" }],
      }).html,
    ).toContain("1 inlay hint shown");
  });
});
