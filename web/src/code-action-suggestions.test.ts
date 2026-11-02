import { describe, expect, it } from "vitest";
import { codeActionSuggestions } from "./code-action-suggestions";
import type { ChangedFile, TextEdit } from "./types";

const file: ChangedFile = {
  path: "main.go",
  status: "modified",
  additions: 2,
  deletions: 2,
  patch: "@@ -1,3 +1,3 @@\n const fish = 'fish';\n-old\n+const size = 1;\n end",
};
const source = "const fish = 'fish';\nconst size = 1;\nend\n";
function edit(
  line: number,
  character: number,
  endLine: number,
  endCharacter: number,
  newText: string,
): TextEdit {
  return {
    location: { path: "main.go", line, character, endLine, endCharacter },
    newText,
  };
}

describe("code action review suggestions", () => {
  it("merges edits on the same line and preserves unaffected text", () => {
    const drafts = codeActionSuggestions(
      [file],
      [edit(1, 6, 1, 10, "length"), edit(1, 13, 1, 14, "2")],
      { "main.go": source },
    );
    expect(drafts).toHaveLength(1);
    expect(drafts[0]).toMatchObject({
      path: "main.go",
      side: "RIGHT",
      line: 2,
      body: "```suggestion\nconst length = 2;\n```",
    });
  });
  it("preserves multiline replacement ranges and a final blank line", () => {
    const drafts = codeActionSuggestions(
      [file],
      [edit(1, 0, 3, 0, "changed\n\n")],
      { "main.go": source },
    );
    expect(drafts[0]).toMatchObject({
      startLine: 2,
      line: 3,
      body: "```suggestion\nchanged\n\n```",
    });
  });
  it("distinguishes deleting a whole line from replacing it with a blank line", () => {
    expect(
      codeActionSuggestions([file], [edit(1, 0, 2, 0, "")], {
        "main.go": source,
      })[0].body,
    ).toBe("```suggestion\n```");
    expect(
      codeActionSuggestions([file], [edit(1, 0, 2, 0, "\n")], {
        "main.go": source,
      })[0].body,
    ).toBe("```suggestion\n\n```");
  });
  it("anchors EOF insertions on the last existing reviewable line", () => {
    expect(
      codeActionSuggestions([file], [edit(3, 0, 3, 0, "added\n")], {
        "main.go": source,
      })[0],
    ).toMatchObject({ line: 3, body: "```suggestion\nend\nadded\n```" });
  });
  it("rejects an entire action containing any unreviewable file or line", () => {
    expect(() =>
      codeActionSuggestions(
        [file],
        [
          edit(1, 0, 1, 1, "x"),
          {
            ...edit(0, 0, 0, 1, "x"),
            location: { ...edit(0, 0, 0, 1, "x").location, path: "other.go" },
          },
        ],
        { "main.go": source, "other.go": "a" },
      ),
    ).toThrow("without a reviewable");
    const limited = { ...file, patch: "@@ -2 +2 @@\n-old\n+const size = 1;" };
    expect(() =>
      codeActionSuggestions([limited], [edit(0, 0, 0, 1, "x")], {
        "main.go": source,
      }),
    ).toThrow("outside the original diff");
  });
  it("rejects stale, overlapping and split-surrogate edits", () => {
    expect(() =>
      codeActionSuggestions(
        [file],
        [{ ...edit(1, 0, 1, 5, "let"), oldText: "wrong" }],
        { "main.go": source },
      ),
    ).toThrow("does not match");
    expect(() =>
      codeActionSuggestions(
        [file],
        [edit(1, 0, 1, 5, "let"), edit(1, 2, 1, 7, "x")],
        { "main.go": source },
      ),
    ).toThrow("overlapping");
    expect(() =>
      codeActionSuggestions([file], [edit(0, 1, 0, 2, "x")], {
        "main.go": "\u{1f41f}",
      }),
    ).toThrow("Unicode");
  });
});
