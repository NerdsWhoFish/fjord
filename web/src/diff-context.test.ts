import { describe, expect, it } from "vitest";
import { parsePatch } from "./diff";
import { buildDiffContext } from "./diff-context";
import type { DiffContextRow } from "./diff-context";

function gaps(rows: DiffContextRow[]) {
  return rows.filter((row) => row.kind === "gap");
}

function context(rows: DiffContextRow[]) {
  return rows.filter((row) => row.kind === "context");
}

function numbered(count: number) {
  return Array.from({ length: count }, (_, index) => `line ${index + 1}`);
}

describe("omitted diff context", () => {
  it("offers known gaps without guessing that context exists below the file", () => {
    const lines = parsePatch(
      "@@ -5 +5 @@\n-old\n+new\n@@ -10 +10 @@\n-other\n+updated",
    );
    const rows = buildDiffContext(lines);
    expect(gaps(rows)).toEqual([
      { kind: "gap", key: "before:0", label: "Show 4 lines above" },
      {
        kind: "gap",
        key: "between:0:3",
        label: "Show 4 lines between hunks",
      },
    ]);
    expect(rows.filter((row) => row.kind === "original")).toEqual(
      lines.map((line, index) => ({ kind: "original", line, index })),
    );
  });

  it("reveals the closest twenty lines above without changing existing keys", () => {
    const lines = parsePatch("@@ -50 +50 @@\n-old\n+new");
    const content = {
      base: numbered(50).join("\n"),
      head: numbered(50).join("\n"),
    };
    const first = buildDiffContext(lines, content, { "before:0": 20 });
    expect(context(first).map((row) => row.line.oldLine)).toEqual(
      Array.from({ length: 20 }, (_, index) => index + 30),
    );
    expect(first[0]).toMatchObject({ kind: "gap", key: "before:0" });
    const second = buildDiffContext(lines, content, { "before:0": 40 });
    expect(context(second).map((row) => row.line.oldLine)).toEqual(
      Array.from({ length: 40 }, (_, index) => index + 10),
    );
    expect(
      context(second)
        .slice(-20)
        .map((row) => row.key),
    ).toEqual(context(first).map((row) => row.key));
    const all = buildDiffContext(lines, content, { "before:0": 60 });
    expect(context(all)).toHaveLength(49);
    expect(gaps(all)).toEqual([]);
  });

  it("expands a middle gap from the preceding hunk and keeps its control below", () => {
    const lines = parsePatch(
      "@@ -1 +1 @@\n-old\n+new\n@@ -50 +50 @@\n-other\n+updated",
    );
    const content = {
      base: numbered(50).join("\n"),
      head: numbered(50).join("\n"),
    };
    const rows = buildDiffContext(lines, content, { "between:0:3": 20 });
    expect(context(rows).map((row) => row.line.newLine)).toEqual(
      Array.from({ length: 20 }, (_, index) => index + 2),
    );
    const control = rows.findIndex((row) => row.kind === "gap");
    expect(rows[control - 1]).toMatchObject({
      kind: "context",
      line: { newLine: 21 },
    });
    expect(rows[control + 1]).toMatchObject({ kind: "original", index: 3 });
  });

  it("tracks different base and head line offsets below inserted changes", () => {
    const lines = parsePatch("@@ -1 +1,2 @@\n-old\n+new\n+extra");
    const rows = buildDiffContext(
      lines,
      {
        base: "old\ncontext one\ncontext two\n",
        head: "new\nextra\ncontext one\ncontext two\n",
      },
      { "after:0": 20 },
    );
    expect(context(rows).map((row) => row.line)).toEqual([
      { kind: "context", text: "context one", oldLine: 2, newLine: 3 },
      { kind: "context", text: "context two", oldLine: 3, newLine: 4 },
    ]);
    expect(gaps(rows)).toEqual([]);
  });

  it("uses zero-count hunk anchors for insertion-only and deletion-only ranges", () => {
    const inserted = parsePatch("@@ -2,0 +3 @@\n+inserted");
    const insertedRows = buildDiffContext(
      inserted,
      { base: "a\nb\nc", head: "a\nb\ninserted\nc" },
      { "before:0": 20, "after:0": 20 },
    );
    expect(
      context(insertedRows).map((row) => [
        row.line.oldLine,
        row.line.newLine,
        row.line.text,
      ]),
    ).toEqual([
      [1, 1, "a"],
      [2, 2, "b"],
      [3, 4, "c"],
    ]);
    const deleted = parsePatch("@@ -3 +2,0 @@\n-deleted");
    const deletedRows = buildDiffContext(
      deleted,
      { base: "a\nb\ndeleted\nc", head: "a\nb\nc" },
      { "before:0": 20, "after:0": 20 },
    );
    expect(
      context(deletedRows).map((row) => [
        row.line.oldLine,
        row.line.newLine,
        row.line.text,
      ]),
    ).toEqual([
      [1, 1, "a"],
      [2, 2, "b"],
      [4, 3, "c"],
    ]);
  });

  it("does not invent a blank EOF line, but preserves real blank lines", () => {
    const lines = parsePatch("@@ -1 +1 @@\n-old\n+new");
    for (const suffix of ["", "\n"]) {
      const rows = buildDiffContext(
        lines,
        { base: `old\ntail${suffix}`, head: `new\ntail${suffix}` },
        { "after:0": 20 },
      );
      expect(context(rows).map((row) => row.line.text)).toEqual(["tail"]);
      expect(gaps(rows)).toEqual([]);
    }
    const withBlank = buildDiffContext(
      lines,
      { base: "old\ntail\n\n", head: "new\ntail\n\n" },
      { "after:0": 20 },
    );
    expect(context(withBlank).map((row) => row.line.text)).toEqual([
      "tail",
      "",
    ]);
  });

  it("never offers a footer for an entirely added or deleted file", () => {
    const added = parsePatch("@@ -0,0 +1,2 @@\n+a\n+b");
    const deleted = parsePatch(
      "@@ -1,2 +0,0 @@\n-a\n-b\n\\ No newline at end of file",
    );
    expect(gaps(buildDiffContext(added))).toEqual([]);
    expect(gaps(buildDiffContext(deleted))).toEqual([]);
    const addedRows = buildDiffContext(
      added,
      { head: "a\nb\n" },
      { "after:0": 20 },
    );
    const deletedRows = buildDiffContext(
      deleted,
      { base: "a\nb" },
      { "after:0": 20 },
    );
    expect(gaps(addedRows)).toEqual([]);
    expect(context(addedRows)).toEqual([]);
    expect(gaps(deletedRows)).toEqual([]);
    expect(context(deletedRows)).toEqual([]);
    expect(deletedRows.at(-1)).toMatchObject({
      kind: "original",
      line: { kind: "notice" },
    });
  });

  it("keeps a gap while a required revision remains unloaded", () => {
    const lines = parsePatch("@@ -3 +3 @@\n-old\n+new");
    const rows = buildDiffContext(
      lines,
      { base: "a\nb\nold\ntail" },
      { "before:0": 20, "after:0": 20 },
    );
    expect(context(rows)).toEqual([]);
    expect(gaps(rows).map((row) => row.key)).toEqual(["before:0"]);
  });

  it("offers only the verified number of lines below and removes the control at EOF", () => {
    const lines = parsePatch("@@ -1 +1 @@\n-old\n+new");
    expect(gaps(buildDiffContext(lines))).toEqual([]);
    expect(
      gaps(buildDiffContext(lines, { base: "old\n", head: "new\n" })),
    ).toEqual([]);
    const content = { base: "old\none\ntwo\n", head: "new\none\ntwo\n" };
    expect(gaps(buildDiffContext(lines, content))).toEqual([
      { kind: "gap", key: "after:0", label: "Show 2 lines below" },
    ]);
    const expanded = buildDiffContext(lines, content, { "after:0": 20 });
    expect(context(expanded).map((row) => row.line.text)).toEqual([
      "one",
      "two",
    ]);
    expect(gaps(expanded)).toEqual([]);
  });

  it("shows only the matching suffix above and prefix below changed hunks", () => {
    const lines = parsePatch("@@ -4 +4 @@\n-old\n+new");
    const rows = buildDiffContext(
      lines,
      {
        base: "same\nbase mismatch\nnear\nold\ntail\nbase mismatch\nfar",
        head: "same\nhead mismatch\nnear\nnew\ntail\nhead mismatch\nfar",
      },
      { "before:0": 20, "after:0": 20 },
    );
    expect(context(rows).map((row) => row.line.text)).toEqual(["near", "tail"]);
    expect(gaps(rows)).toEqual([]);
  });

  it("preserves CRLF source text and original indices for renamed-file diffs", () => {
    const lines = parsePatch("@@ -2 +2 @@\n-old\r\n+new\r");
    const rows = buildDiffContext(
      lines,
      {
        base: "before\r\nold\r\nafter\r\n",
        head: "before\r\nnew\r\nafter\r\n",
      },
      { "before:0": 20, "after:0": 20 },
    );
    expect(context(rows).map((row) => row.line.text)).toEqual([
      "before\r",
      "after\r",
    ]);
    expect(
      rows.filter((row) => row.kind === "original").map((row) => row.index),
    ).toEqual([0, 1, 2]);
  });

  it("leaves patches without usable hunk boundaries alone", () => {
    const notices = parsePatch("Binary files differ");
    expect(buildDiffContext(notices)).toEqual([
      { kind: "original", index: 0, line: notices[0] },
    ]);
    const invalid = [
      { kind: "hunk" as const, text: "@@ -999999999999999999999 +1 @@" },
    ];
    expect(gaps(buildDiffContext(invalid))).toEqual([]);
    const overlapping = parsePatch(
      "@@ -2,2 +2,2 @@\n a\n b\n@@ -3 +3 @@\n-old\n+new",
    );
    expect(gaps(buildDiffContext(overlapping))).toEqual([]);
    expect(buildDiffContext([])).toEqual([]);
  });

  it("does not shift an above-hunk anchor when loaded source is too short", () => {
    const lines = parsePatch("@@ -50 +50 @@\n-old\n+new");
    const rows = buildDiffContext(
      lines,
      { base: "one\ntwo", head: "one\ntwo" },
      { "before:0": 20 },
    );
    expect(context(rows)).toEqual([]);
    expect(gaps(rows)).toEqual([]);
  });
});
