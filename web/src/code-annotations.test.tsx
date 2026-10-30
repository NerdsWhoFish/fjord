import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { annotationParts, annotationsForLine } from "./code-annotations";
import { AnnotatedText } from "./AnnotatedText";
import { tokenize } from "./diff";
import type { CodeAnnotations, CodeLocation } from "./types";

const location = (
  line: number,
  character: number,
  endLine = line,
  endCharacter = character,
): CodeLocation => ({
  path: "main.go",
  line,
  character,
  endLine,
  endCharacter,
});

describe("inline language-server annotations", () => {
  it("isolates annotations by exact path and revision", () => {
    const annotations: CodeAnnotations = {
      path: "main.go",
      sha: "head",
      highlights: [{ location: location(0, 0, 0, 3) }],
      hints: [{ location: location(0, 3), label: ": int" }],
    };
    expect(
      annotationsForLine(annotations, "other.go", "head", 0, "foo"),
    ).toEqual({ highlights: [], hints: [] });
    expect(
      annotationsForLine(annotations, "main.go", "base", 0, "foo"),
    ).toEqual({ highlights: [], hints: [] });
    expect(
      annotationsForLine(annotations, "main.go", "head", 1, "foo"),
    ).toEqual({ highlights: [], hints: [] });
    expect(
      annotationsForLine(annotations, "main.go", "head", 0, "foo").highlights,
    ).toEqual([{ start: 0, end: 3, kind: "text" }]);
  });

  it("clips multiline highlights to each original line and preserves write kind", () => {
    const annotations: CodeAnnotations = {
      path: "main.go",
      sha: "head",
      highlights: [{ location: location(1, 2, 3, 0), kind: "write" }],
    };
    expect(
      annotationsForLine(annotations, "main.go", "head", 1, "abcdef")
        .highlights,
    ).toEqual([{ start: 2, end: 6, kind: "write" }]);
    expect(
      annotationsForLine(annotations, "main.go", "head", 2, "abc").highlights,
    ).toEqual([{ start: 0, end: 3, kind: "write" }]);
    expect(
      annotationsForLine(annotations, "main.go", "head", 3, "abc").highlights,
    ).toEqual([]);
  });

  it("inserts boundary hints once without changing token UTF-16 offsets or source", () => {
    const code = "fish(\u{1f41f});";
    const annotations: CodeAnnotations = {
      path: "main.go",
      sha: "head",
      hints: [
        { location: location(0, 0), label: "start" },
        { location: location(0, 4), label: ": fn" },
        { location: location(0, code.length), label: "end" },
      ],
      highlights: [{ location: location(0, 0, 0, 4), kind: "read" }],
    };
    const line = annotationsForLine(annotations, "main.go", "head", 0, code);
    const tokens = tokenize(code);
    const parts = tokens.flatMap((token, index) =>
      annotationParts(
        token.text,
        token.column,
        line,
        index === tokens.length - 1,
      ),
    );
    expect(
      parts.flatMap((part) => (part.kind === "hint" ? [part.hint.label] : [])),
    ).toEqual(["start", ": fn", "end"]);
    const text = parts.flatMap((part) => (part.kind === "text" ? [part] : []));
    expect(text.map((part) => part.text).join("")).toBe(code);
    for (const part of text)
      expect(code.slice(part.start, part.start + part.text.length)).toBe(
        part.text,
      );
  });

  it("does not split a surrogate pair when a server supplies a mid-pair position", () => {
    const code = "a\u{1f41f}b";
    const annotations: CodeAnnotations = {
      path: "main.go",
      sha: "head",
      hints: [{ location: location(0, 2), label: "hint" }],
      highlights: [{ location: location(0, 2, 0, 3) }],
    };
    const line = annotationsForLine(annotations, "main.go", "head", 0, code);
    expect(line.hints[0].position).toBe(3);
    expect(line.highlights).toEqual([{ start: 1, end: 3, kind: "text" }]);
    expect(
      annotationParts(code, 0, line, true)
        .filter((part) => part.kind === "text")
        .map((part) => part.text),
    ).toEqual(["a", "\u{1f41f}", "b"]);
  });

  it("keeps hints on empty lines and discards invalid or foreign positions", () => {
    const annotations: CodeAnnotations = {
      path: "main.go",
      sha: "head",
      hints: [
        { location: location(0, 0), label: "empty" },
        { location: location(0, 3), label: "outside" },
        {
          location: { ...location(0, 0), path: "foreign.go" },
          label: "foreign",
        },
      ],
      highlights: [{ location: location(-1, 0, 0, 1) }],
    };
    const line = annotationsForLine(annotations, "main.go", "head", 0, "");
    expect(line.highlights).toEqual([]);
    expect(annotationParts("", 0, line, true)).toEqual([
      { kind: "hint", hint: annotations.hints![0] },
    ]);
  });

  it("renders syntax, highlights, and untrusted hint text safely together", () => {
    const html = renderToStaticMarkup(
      <AnnotatedText
        text="value"
        spans={[{ start: 0, end: 5, kind: "keyword" }]}
        annotations={{
          highlights: [{ start: 0, end: 5, kind: "write" }],
          hints: [
            {
              position: 5,
              hint: {
                location: location(0, 5),
                label: "<script>bad()</script>",
              },
            },
          ],
        }}
        includeEnd
      />,
    );
    expect(html).toContain('class="code-document-highlight highlight-write"');
    expect(html).toContain('class="syntax-keyword"');
    expect(html).toContain("&lt;script&gt;bad()&lt;/script&gt;");
    expect(html).not.toContain("<script>");
  });
});
