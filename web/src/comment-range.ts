import type { DiffLine } from "./diff";
import type { Side } from "./types";

export interface LinePoint {
  row: number;
  hunk: number;
  side: Side;
  line: number;
  text: string;
}
export type IndexedLine = Partial<Record<Side, LinePoint>>;
export interface CommentRange {
  first: LinePoint;
  last: LinePoint;
  points: LinePoint[];
  code: string;
}
export type RangeResult =
  { range: CommentRange; error?: never } | { error: string; range?: never };

export function indexCommentLines(lines: DiffLine[]): IndexedLine[] {
  let hunk = -1;
  return lines.map((line, row) => {
    if (line.kind === "hunk") hunk++;
    const indexed: IndexedLine = {};
    if (hunk < 0 || line.kind === "hunk" || line.kind === "notice")
      return indexed;
    if (line.oldLine !== undefined)
      indexed.LEFT = {
        row,
        hunk,
        side: "LEFT",
        line: line.oldLine,
        text: line.text,
      };
    if (line.newLine !== undefined)
      indexed.RIGHT = {
        row,
        hunk,
        side: "RIGHT",
        line: line.newLine,
        text: line.text,
      };
    return indexed;
  });
}

export function commentRange(
  indexed: IndexedLine[],
  anchor: LinePoint,
  focus: LinePoint,
): RangeResult {
  if (anchor.side !== focus.side)
    return { error: "Keep the selection on the same revision side." };
  if (anchor.hunk !== focus.hunk)
    return { error: "Select lines within one diff hunk." };
  const [first, last] =
    anchor.line <= focus.line ? [anchor, focus] : [focus, anchor];
  if (last.line - first.line >= 200)
    return { error: "Select up to 200 lines per comment." };
  const points = indexed
    .slice(first.row, last.row + 1)
    .flatMap((row) => (row[first.side] ? [row[first.side]!] : []));
  if (
    !points.length ||
    points[0].line !== first.line ||
    points.at(-1)?.line !== last.line ||
    points.some(
      (point, i) => point.hunk !== first.hunk || point.line !== first.line + i,
    )
  ) {
    return {
      error: "The range must contain consecutive lines from the same revision.",
    };
  }
  let code = points.map((point) => point.text).join("\n");
  if (code.length > 32768) {
    code =
      code.slice(0, 32768).replace(/[\uD800-\uDBFF]$/, "") +
      "\n[Preview limited to 32,768 characters. The comment still covers the selected lines.]";
  }
  return { range: { first, last, points, code } };
}

export function rangeFields(range: CommentRange): {
  line: number;
  side: Side;
  startLine?: number;
  startSide?: Side;
} {
  return range.first.line === range.last.line
    ? { line: range.last.line, side: range.last.side }
    : {
        line: range.last.line,
        side: range.last.side,
        startLine: range.first.line,
        startSide: range.first.side,
      };
}

export function commentLineLabel(line: number, startLine?: number): string {
  return startLine !== undefined && startLine < line
    ? `${startLine}-${line}`
    : String(line);
}
