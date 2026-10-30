import type { Side } from "./types";

export interface DiffLine {
  kind: "context" | "add" | "delete" | "hunk" | "notice";
  text: string;
  oldLine?: number;
  newLine?: number;
}

export function parsePatch(patch: string): DiffLine[] {
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;
  return patch.split("\n").flatMap((text): DiffLine[] => {
    const match = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(text);
    if (match) {
      oldLine = Number(match[1]);
      newLine = Number(match[2]);
      inHunk = true;
      return [{ kind: "hunk", text }];
    }
    if (!inHunk || text.startsWith("\\"))
      return text ? [{ kind: "notice", text }] : [];
    if (text.startsWith("+"))
      return [{ kind: "add", text: text.slice(1), newLine: newLine++ }];
    if (text.startsWith("-"))
      return [{ kind: "delete", text: text.slice(1), oldLine: oldLine++ }];
    if (text.startsWith(" "))
      return [
        {
          kind: "context",
          text: text.slice(1),
          oldLine: oldLine++,
          newLine: newLine++,
        },
      ];
    return [];
  });
}

export function linePosition(
  line: DiffLine,
): { line: number; side: Side } | undefined {
  if (line.kind === "delete" && line.oldLine !== undefined)
    return { line: line.oldLine, side: "LEFT" };
  if (line.newLine !== undefined) return { line: line.newLine, side: "RIGHT" };
}

// JavaScript string offsets are UTF-16 code units, matching the LSP default.
export function tokenize(
  code: string,
): { text: string; column: number; symbol: boolean }[] {
  const parts: { text: string; column: number; symbol: boolean }[] = [];
  const matcher = /[\p{L}_][\p{L}\p{N}_]*/gu;
  let end = 0;
  for (const match of code.matchAll(matcher)) {
    const column = match.index;
    if (column > end)
      parts.push({ text: code.slice(end, column), column: end, symbol: false });
    parts.push({ text: match[0], column, symbol: true });
    end = column + match[0].length;
  }
  if (end < code.length)
    parts.push({ text: code.slice(end), column: end, symbol: false });
  return parts;
}
