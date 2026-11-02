import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { parsePatch } from "./diff";
import { diffLayout, reviewThreads } from "./diff-layout";
import {
  InlineDraftComments,
  InlineReviewThreads,
} from "./InlineReviewThreads";
import type { DiffContextRow } from "./diff-context";
import type { ThreadComment } from "./types";

const rows = (patch: string): DiffContextRow[] =>
  parsePatch(patch).map((line, index) => ({ kind: "original", line, index }));
const comment = (
  id: number,
  extra: Partial<ThreadComment> = {},
): ThreadComment => ({
  id,
  path: "main.go",
  line: 5,
  side: "RIGHT",
  author: "reviewer",
  body: "Please explain this.",
  url: "",
  ...extra,
});

describe("split diff alignment", () => {
  it("pairs replacement lines, preserves unequal runs and duplicates context by side", () => {
    const layout = diffLayout(
      rows("@@ -4,4 +4,3 @@\n before\n-old one\n-old two\n+new\n after"),
      true,
    );
    expect(
      layout.map(({ index, side, track }) => [index, side, track]),
    ).toEqual([
      [0, undefined, 1],
      [1, "LEFT", 2],
      [1, "RIGHT", 2],
      [2, "LEFT", 3],
      [4, "RIGHT", 3],
      [3, "LEFT", 4],
      [5, "LEFT", 5],
      [5, "RIGHT", 5],
    ]);
  });
  it("does not pair additions across context gaps or hunks", () => {
    const initial = rows("@@ -1 +1,0 @@\n-gone");
    const more = rows("@@ -10,0 +10 @@\n+new");
    const layout = diffLayout(
      [...initial, { kind: "gap", key: "gap", label: "More" }, ...more],
      true,
    );
    const left = layout.find((entry) => entry.side === "LEFT")!;
    const right = layout.find((entry) => entry.side === "RIGHT")!;
    expect(left.track).toBeLessThan(right.track);
    expect(
      layout.find((entry) => entry.row.kind === "gap")?.side,
    ).toBeUndefined();
  });
  it("leaves unified source order and syntax indices unchanged", () => {
    const original = rows("@@ -1 +1 @@\n-old\n+new");
    expect(
      diffLayout(original, false).map(({ row, index, side }) => [
        row,
        index,
        side,
      ]),
    ).toEqual(original.map((row, index) => [row, index, undefined]));
  });
});

describe("inline review threads", () => {
  it("shows local drafts distinctly with AI attribution and an edit action", () => {
    const markup = renderToStaticMarkup(
      <InlineDraftComments
        drafts={[
          {
            id: "draft-1",
            path: "main.go",
            side: "LEFT",
            startLine: 3,
            line: 5,
            body: "Suggested change",
            source: "ai",
          },
        ]}
        onReviewDrafts={() => {}}
      />,
    );
    expect(markup).toContain("Draft");
    expect(markup).toContain("AI suggestion");
    expect(markup).toContain("lines 3-5");
    expect(markup).toContain(">Edit draft</button>");
    expect(markup).not.toContain(">Reply</button>");
  });
  it("groups replies under the root anchor regardless of response ordering", () => {
    const root = comment(1, { side: "LEFT", startLine: 3 });
    const reply = comment(2, { inReplyToId: 1, line: 0 });
    expect(reviewThreads([reply, root])).toEqual([
      { root, comments: [root, reply] },
    ]);
  });
  it("never attaches outdated, orphaned or cyclic threads to live lines", () => {
    expect(
      reviewThreads([
        comment(1, { outdated: true }),
        comment(2, { line: 0 }),
        comment(3, { inReplyToId: 99 }),
        comment(4, { inReplyToId: 5 }),
        comment(5, { inReplyToId: 4 }),
      ]),
    ).toEqual([]);
  });
  it("renders multiline revision labels and escapes untrusted content", () => {
    const markup = renderToStaticMarkup(
      <InlineReviewThreads
        threads={reviewThreads([
          comment(1, {
            side: "LEFT",
            startLine: 3,
            body: "<script>bad()</script>",
          }),
        ])}
        onReply={async () => {}}
      />,
    );
    expect(markup).toContain("Review thread on base lines 3-5");
    expect(markup).not.toContain("bad()");
    expect(markup).not.toContain("<script>");
    expect(markup).toContain(">Reply</button>");
  });
});
