import { describe, expect, it } from "vitest";
import { parsePatch } from "./diff";
import {
  commentLineLabel,
  commentRange,
  indexCommentLines,
  rangeFields,
} from "./comment-range";

const patch = [
  "@@ -10,4 +20,5 @@",
  " before",
  "-oldFirst()",
  "-oldSecond()",
  "+newFirst()",
  "+newSecond()",
  "+newThird()",
  " after",
  "@@ -40,2 +60,2 @@",
  " nextHunk()",
  " last()",
].join("\n");

describe("multiline comment ranges", () => {
  const indexed = indexCommentLines(parsePatch(patch));

  it("normalizes reverse dragging and keeps all selected head lines", () => {
    const result = commentRange(indexed, indexed[7].RIGHT!, indexed[4].RIGHT!);
    expect(result.error).toBeUndefined();
    expect(rangeFields(result.range!)).toEqual({
      startLine: 21,
      startSide: "RIGHT",
      line: 24,
      side: "RIGHT",
    });
    expect(result.range?.code).toBe(
      "newFirst()\nnewSecond()\nnewThird()\nafter",
    );
  });

  it("keeps deleted base context separate from head additions", () => {
    const result = commentRange(indexed, indexed[1].LEFT!, indexed[7].LEFT!);
    expect(result.range?.code).toBe("before\noldFirst()\noldSecond()\nafter");
    expect(rangeFields(result.range!)).toEqual({
      startLine: 10,
      startSide: "LEFT",
      line: 13,
      side: "LEFT",
    });
  });

  it("rejects cross-side and cross-hunk selections", () => {
    expect(
      commentRange(indexed, indexed[2].LEFT!, indexed[4].RIGHT!).error,
    ).toMatch(/same revision side/);
    expect(
      commentRange(indexed, indexed[4].RIGHT!, indexed[9].RIGHT!).error,
    ).toMatch(/one diff hunk/);
  });

  it("omits range fields for one line", () => {
    const point = indexed[2].LEFT!;
    const result = commentRange(indexed, point, point);
    expect(rangeFields(result.range!)).toEqual({ line: 11, side: "LEFT" });
    expect(commentLineLabel(11)).toBe("11");
    expect(commentLineLabel(13, 10)).toBe("10-13");
  });

  it("rejects more than 200 lines instead of changing the selected endpoint", () => {
    const large = indexCommentLines(
      parsePatch(
        "@@ -0,0 +1,201 @@\n" +
          Array.from({ length: 201 }, () => "+item()").join("\n"),
      ),
    );
    expect(
      commentRange(large, large[1].RIGHT!, large[200].RIGHT!).range?.points,
    ).toHaveLength(200);
    expect(
      commentRange(large, large[1].RIGHT!, large[201].RIGHT!).error,
    ).toMatch(/200 lines/);
  });

  it("bounds oversized previews without changing the review range", () => {
    const long = indexCommentLines(
      parsePatch("@@ -0,0 +1,2 @@\n+" + "a".repeat(32767) + "\u{1f41f}\n+last"),
    );
    const result = commentRange(long, long[1].RIGHT!, long[2].RIGHT!);
    expect(rangeFields(result.range!)).toEqual({
      startLine: 1,
      startSide: "RIGHT",
      line: 2,
      side: "RIGHT",
    });
    expect(result.range!.code.length).toBeLessThan(33000);
    expect(result.range!.code).toContain("Preview limited");
    expect(result.range!.code).not.toMatch(/[\uD800-\uDBFF]\n/);
  });

  it("does not make hunk or no-newline markers selectable", () => {
    const rows = indexCommentLines(
      parsePatch("@@ -1 +1 @@\n-a\n+b\n\\ No newline at end of file"),
    );
    expect(rows[0]).toEqual({});
    expect(rows[3]).toEqual({});
  });
});
