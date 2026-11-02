import type { DiffContextRow } from "./diff-context";
import type { Side, ThreadComment } from "./types";

export interface DiffEntry {
  row: DiffContextRow;
  index: number;
  side?: Side;
  track: number;
}

export function diffLayout(
  rows: DiffContextRow[],
  split: boolean,
): DiffEntry[] {
  if (!split)
    return rows.map((row, index) => ({ row, index, track: index + 1 }));
  const entries: DiffEntry[] = [];
  let track = 1;
  for (let index = 0; index < rows.length; ) {
    const row = rows[index];
    if (
      row.kind === "gap" ||
      row.line.kind === "hunk" ||
      row.line.kind === "notice"
    ) {
      entries.push({ row, index, track: track++ });
      index++;
    } else if (row.line.kind === "context") {
      entries.push(
        { row, index, side: "LEFT", track },
        { row, index, side: "RIGHT", track },
      );
      track++;
      index++;
    } else {
      const deleted: number[] = [];
      const added: number[] = [];
      while (index < rows.length) {
        const next = rows[index];
        if (next.kind === "gap" || next.line.kind !== "delete") break;
        deleted.push(index++);
      }
      while (index < rows.length) {
        const next = rows[index];
        if (next.kind === "gap" || next.line.kind !== "add") break;
        added.push(index++);
      }
      for (
        let offset = 0;
        offset < Math.max(deleted.length, added.length);
        offset++
      ) {
        if (deleted[offset] !== undefined)
          entries.push({
            row: rows[deleted[offset]],
            index: deleted[offset],
            side: "LEFT",
            track,
          });
        if (added[offset] !== undefined)
          entries.push({
            row: rows[added[offset]],
            index: added[offset],
            side: "RIGHT",
            track,
          });
        track++;
      }
    }
  }
  return entries;
}

export interface ReviewThread {
  root: ThreadComment;
  comments: ThreadComment[];
}

type CommentMetadata = ThreadComment & {
  inReplyToId?: number;
  outdated?: boolean;
};

export function reviewThreads(comments: ThreadComment[]): ReviewThread[] {
  const byId = new Map(
    comments.map((comment) => [comment.id, comment as CommentMetadata]),
  );
  const grouped = new Map<number, ReviewThread>();
  for (const comment of comments as CommentMetadata[]) {
    let root = comment;
    const seen = new Set<number>();
    while (
      root.inReplyToId &&
      byId.has(root.inReplyToId) &&
      !seen.has(root.id)
    ) {
      seen.add(root.id);
      root = byId.get(root.inReplyToId)!;
    }
    // An unavailable parent cannot be safely assigned to a current diff line.
    if (root.inReplyToId || root.outdated || root.line <= 0) continue;
    let thread = grouped.get(root.id);
    if (!thread) {
      thread = { root, comments: [] };
      grouped.set(root.id, thread);
    }
    thread.comments.push(comment);
  }
  return [...grouped.values()].map((thread) => ({
    ...thread,
    comments: [...thread.comments].sort((a, b) =>
      a.id === thread.root.id ? -1 : b.id === thread.root.id ? 1 : a.id - b.id,
    ),
  }));
}
