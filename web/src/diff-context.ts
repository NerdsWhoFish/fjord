import type { DiffLine } from "./diff";

export interface DiffContextContent {
  base?: string;
  head?: string;
}

export type DiffContextRow =
  | { kind: "original"; index: number; line: DiffLine }
  | { kind: "context"; key: string; line: DiffLine }
  | { kind: "gap"; key: string; label: string };

interface Hunk {
  index: number;
  oldBefore: number;
  newBefore: number;
  oldAfter: number;
  newAfter: number;
}

interface Gap {
  key: string;
  direction: "above" | "between" | "below";
  oldStart: number;
  newStart: number;
  count?: number;
}

const chunkSize = 20;

function readHunk(line: DiffLine, index: number): Hunk | undefined {
  const match = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/.exec(line.text);
  if (!match) return undefined;
  const oldStart = Number(match[1]);
  const oldCount = match[2] === undefined ? 1 : Number(match[2]);
  const newStart = Number(match[3]);
  const newCount = match[4] === undefined ? 1 : Number(match[4]);
  if (
    ![oldStart, oldCount, newStart, newCount].every(Number.isSafeInteger) ||
    (oldStart === 0 && oldCount !== 0) ||
    (newStart === 0 && newCount !== 0)
  )
    return undefined;
  // A zero-length range is anchored after its numbered line, unlike a
  // nonempty range whose number identifies the first line in the hunk.
  const oldBefore = oldStart + (oldCount === 0 ? 1 : 0);
  const newBefore = newStart + (newCount === 0 ? 1 : 0);
  const oldAfter = oldBefore + oldCount;
  const newAfter = newBefore + newCount;
  if (![oldAfter, newAfter].every(Number.isSafeInteger)) return undefined;
  return { index, oldBefore, newBefore, oldAfter, newAfter };
}

function sourceLines(content: string | undefined): string[] | undefined {
  if (content === undefined) return undefined;
  if (content === "") return [];
  const lines = content.split("\n");
  if (lines.at(-1) === "") lines.pop();
  return lines;
}

function contextRows(
  gap: Gap,
  base: string[] | undefined,
  head: string[] | undefined,
  requested: number,
): DiffContextRow[] {
  if (gap.count === 0) return [];
  if (base === undefined || head === undefined) {
    if (gap.count === undefined) return [];
    return [{ kind: "gap", key: gap.key, label: gapLabel(gap, gap.count) }];
  }
  const limit = Math.max(
    0,
    Math.min(
      gap.count ?? Number.MAX_SAFE_INTEGER,
      base.length - gap.oldStart + 1,
      head.length - gap.newStart + 1,
    ),
  );
  if (gap.direction === "above" && limit !== gap.count) return [];
  let available = 0;
  for (; available < limit; available++) {
    const offset =
      gap.direction === "above" ? limit - available - 1 : available;
    if (base[gap.oldStart - 1 + offset] !== head[gap.newStart - 1 + offset])
      break;
  }
  const count = Number.isFinite(requested)
    ? Math.min(available, Math.max(0, Math.floor(requested)))
    : 0;
  const first = gap.direction === "above" ? limit - count : 0;
  const rows: DiffContextRow[] = [];
  for (let offset = first; offset < first + count; offset++) {
    const oldLine = gap.oldStart + offset;
    const newLine = gap.newStart + offset;
    rows.push({
      kind: "context",
      key: `${gap.key}:${oldLine}:${newLine}`,
      line: {
        kind: "context",
        text: base[oldLine - 1],
        oldLine,
        newLine,
      },
    });
  }
  if (count < available) {
    const control: DiffContextRow = {
      kind: "gap",
      key: gap.key,
      label: gapLabel(gap, available - count),
    };
    if (gap.direction === "above") rows.unshift(control);
    else rows.push(control);
  }
  return rows;
}

function gapLabel(gap: Gap, remaining?: number): string {
  const count = Math.min(chunkSize, remaining ?? chunkSize);
  const lines = `${count} ${count === 1 ? "line" : "lines"}`;
  return gap.direction === "between"
    ? `Show ${lines} between hunks`
    : `Show ${lines} ${gap.direction}`;
}

export function buildDiffContext(
  lines: DiffLine[],
  content: DiffContextContent = {},
  expanded: Record<string, number> = {},
): DiffContextRow[] {
  const originals = lines.map((line, index): DiffContextRow => ({
    kind: "original",
    index,
    line,
  }));
  const hunks: Hunk[] = [];
  for (let index = 0; index < lines.length; index++) {
    if (lines[index].kind !== "hunk") continue;
    const hunk = readHunk(lines[index], index);
    const previous = hunks.at(-1);
    if (
      !hunk ||
      (previous &&
        (hunk.oldBefore < previous.oldAfter ||
          hunk.newBefore < previous.newAfter))
    )
      return originals;
    hunks.push(hunk);
  }
  if (!hunks.length) return originals;
  let base = sourceLines(content.base);
  let head = sourceLines(content.head);
  // A zero-length absent side cannot have unchanged context to reveal.
  if (
    base === undefined &&
    hunks.every((hunk) => hunk.oldBefore === 1 && hunk.oldAfter === 1)
  )
    base = [];
  if (
    head === undefined &&
    hunks.every((hunk) => hunk.newBefore === 1 && hunk.newAfter === 1)
  )
    head = [];
  const insertions = new Map<number, DiffContextRow[]>();
  const first = hunks[0];
  const beforeCount = Math.max(
    0,
    Math.min(first.oldBefore - 1, first.newBefore - 1),
  );
  const before: Gap = {
    key: `before:${first.index}`,
    direction: "above",
    oldStart: first.oldBefore - beforeCount,
    newStart: first.newBefore - beforeCount,
    count: beforeCount,
  };
  insertions.set(
    first.index,
    contextRows(before, base, head, expanded[before.key] ?? 0),
  );
  for (let i = 1; i < hunks.length; i++) {
    const previous = hunks[i - 1];
    const next = hunks[i];
    const between: Gap = {
      key: `between:${previous.index}:${next.index}`,
      direction: "between",
      oldStart: previous.oldAfter,
      newStart: previous.newAfter,
      count: Math.min(
        next.oldBefore - previous.oldAfter,
        next.newBefore - previous.newAfter,
      ),
    };
    insertions.set(
      next.index,
      contextRows(between, base, head, expanded[between.key] ?? 0),
    );
  }
  const last = hunks.at(-1)!;
  const after: Gap = {
    key: `after:${last.index}`,
    direction: "below",
    oldStart: last.oldAfter,
    newStart: last.newAfter,
  };
  const result: DiffContextRow[] = [];
  for (const row of originals) {
    if (row.kind !== "original") continue;
    for (const inserted of insertions.get(row.index) ?? [])
      result.push(inserted);
    result.push(row);
  }
  for (const row of contextRows(after, base, head, expanded[after.key] ?? 0))
    result.push(row);
  return result;
}
