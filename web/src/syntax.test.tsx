import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { highlightRequest, languageFor } from "./syntax";
import { parsePatch, tokenize } from "./diff";
import { SyntaxText } from "./SyntaxText";
import { useSyntax } from "./useSyntax";

describe("syntax colors without changing reviewed code", () => {
  it("renders safely before a source pane or worker result exists", () => {
    function UnopenedSource() {
      const result = useSyntax();
      return <span>{result.lines.length}</span>;
    }
    expect(renderToStaticMarkup(<UnopenedSource />)).toBe("<span>0</span>");
  });
  it("preserves multiline comments and raw strings across source lines", () => {
    const result = highlightRequest({
      kind: "source",
      path: "example.go",
      code: "/* first\nsecond */\nvar text = `hello\nworld`",
    });
    expect(result.lines[1][0].kind).toBe("comment");
    expect(result.lines[3][0].kind).toBe("string");
  });

  it("keeps old and new lexical state separate and resets at diff hunks", () => {
    const lines = parsePatch(
      "@@ -1,2 +1,1 @@\n-/* open\n-var before = 1\n+var after = 2\n@@ -10 +10 @@\n-var previous = 3\n+var next = 4",
    );
    const result = highlightRequest({
      kind: "diff",
      path: "example.go",
      lines,
    });
    expect(result.lines[2][0].kind).toBe("comment");
    expect(result.lines[3][0].kind).toBe("keyword");
    expect(result.lines[5][0].kind).toBe("keyword");
  });

  it("handles Terraform, filenames, unknown types, and large-input fallback", () => {
    expect(languageFor("modules/main.tf")).toBe("hcl");
    expect(languageFor("Dockerfile.dev")).toBe("docker");
    expect(languageFor("Component.TSX")).toBe("tsx");
    expect(
      highlightRequest({ kind: "source", path: "foo.unknown", code: "foo" })
        .lines,
    ).toEqual([]);
    expect(
      highlightRequest({
        kind: "source",
        path: "large.go",
        code: "x".repeat(250001),
      }).limited,
    ).toBe(true);
    const hcl = highlightRequest({
      kind: "source",
      path: "main.tf",
      code: 'resource "example" "demo" {\n  count = 2\n}',
    });
    expect(hcl.lines[0].some((span) => span.kind === "string")).toBe(true);
    expect(hcl.lines[1].some((span) => span.kind === "number")).toBe(true);
  });

  it("preserves UTF-16 click positions while rendering HTML-like code as text", () => {
    const code = 'const fish = "🐟"; Reader.Read("<script>alert(1)</script>");';
    const { lines } = highlightRequest({
      kind: "source",
      path: "code.ts",
      code,
    });
    const reader = tokenize(code).find((token) => token.text === "Reader")!;
    expect(reader.column).toBe(code.indexOf("Reader"));
    const markup = renderToStaticMarkup(
      <SyntaxText text={code} spans={lines[0]} />,
    );
    expect(markup).not.toContain("<script>");
    expect(markup).toContain("&lt;script&gt;");
    const tokenMarkup = renderToStaticMarkup(
      <SyntaxText text={reader.text} start={reader.column} spans={lines[0]} />,
    );
    expect(tokenMarkup.replace(/<[^>]+>/g, "")).toBe("Reader");
  });
});
