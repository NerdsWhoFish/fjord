import { describe, expect, it } from "vitest";
import { linePosition, parsePatch, tokenize } from "./diff";

describe("revision-aware diff positions", () => {
  it("tracks deleted base lines and added head lines across hunks", () => {
    const lines = parsePatch(
      "@@ -7,3 +7,3 @@ func run() {\n before\n-old()\n+next()\n after\n@@ -20 +24 @@\n-removed\n+added",
    );
    expect(linePosition(lines[1])).toEqual({ line: 7, side: "RIGHT" });
    expect(linePosition(lines[2])).toEqual({ line: 8, side: "LEFT" });
    expect(linePosition(lines[3])).toEqual({ line: 8, side: "RIGHT" });
    expect(linePosition(lines[6])).toEqual({ line: 20, side: "LEFT" });
    expect(linePosition(lines[7])).toEqual({ line: 24, side: "RIGHT" });
    expect(linePosition(lines[0])).toBeUndefined();
  });
  it("does not treat no-newline notices as commentable code", () => {
    const lines = parsePatch(
      "@@ -1 +0,0 @@\n-deleted\n\\ No newline at end of file",
    );
    expect(lines[2].kind).toBe("notice");
    expect(linePosition(lines[2])).toBeUndefined();
  });
  it("preserves UTF-16 token columns after astral characters", () => {
    const tokens = tokenize('"\u{1F41F}"; Reader.Read()');
    expect(tokens.find((t) => t.text === "Reader")?.column).toBe(6);
    expect(tokens.map((t) => t.text).join("")).toBe(
      '"\u{1F41F}"; Reader.Read()',
    );
  });
});
