import { parsePatch } from "./diff";
import { commentRange, indexCommentLines, rangeFields } from "./comment-range";
import { insertSuggestion } from "./suggestion";
import type { ChangedFile, DraftComment, TextEdit } from "./types";

export function codeActionSuggestions(
  files: ChangedFile[],
  edits: TextEdit[],
  contents: Record<string, string>,
): Omit<DraftComment, "id">[] {
  if (!edits.length || edits.length > 100)
    throw new Error("A review fix must contain between 1 and 100 edits.");
  const result: Omit<DraftComment, "id">[] = [];
  for (const path of [
    ...new Set(edits.map((edit) => edit.location.path)),
  ].sort()) {
    const file = files.find(
      (candidate) => candidate.path === path && candidate.status !== "removed",
    );
    const content = contents[path];
    if (!file?.patch || typeof content !== "string")
      throw new Error(
        "This fix includes a file without a reviewable text diff.",
      );
    const lines = content.split("\n");
    const starts: number[] = [];
    let offset = 0;
    for (const line of lines) {
      starts.push(offset);
      offset += line.length + 1;
    }
    const actualLines = content.endsWith("\n")
      ? lines.length - 1
      : lines.length;
    function position(line: number, character: number) {
      if (
        !Number.isInteger(line) ||
        !Number.isInteger(character) ||
        line < 0 ||
        line >= lines.length ||
        character < 0 ||
        character > lines[line].length
      )
        throw new Error("A fix contains an invalid source position.");
      const previous = lines[line].charCodeAt(character - 1),
        next = lines[line].charCodeAt(character);
      if (
        previous >= 0xd800 &&
        previous <= 0xdbff &&
        next >= 0xdc00 &&
        next <= 0xdfff
      )
        throw new Error("A fix splits a Unicode character.");
      return starts[line] + character;
    }
    const located = edits
      .filter((edit) => edit.location.path === path)
      .map((edit) => {
        const range = edit.location;
        const start = position(range.line, range.character),
          end = position(range.endLine, range.endCharacter);
        if (
          end < start ||
          (edit.oldText !== undefined &&
            content.slice(start, end) !== edit.oldText)
        )
          throw new Error(
            "The fix does not match the reviewed source revision.",
          );
        let first = range.line;
        let last =
          range.endLine > range.line && range.endCharacter === 0
            ? range.endLine - 1
            : range.endLine;
        if (start === end && start === content.length && content.endsWith("\n"))
          first = last = actualLines - 1;
        if (first < 0 || last >= actualLines)
          throw new Error(
            "This insertion has no existing diff line to attach a suggestion to.",
          );
        return { edit, start, end, first, last };
      })
      .sort((a, b) => a.start - b.start || a.end - b.end);
    for (let index = 1; index < located.length; index++)
      if (
        located[index].start < located[index - 1].end ||
        located[index].start === located[index - 1].start
      )
        throw new Error(
          "This fix contains overlapping edits and cannot become suggestions.",
        );
    const groups: { first: number; last: number; edits: typeof located }[] = [];
    for (const edit of located) {
      const previous = groups.at(-1);
      if (previous && edit.first <= previous.last) {
        previous.last = Math.max(previous.last, edit.last);
        previous.edits.push(edit);
      } else groups.push({ first: edit.first, last: edit.last, edits: [edit] });
    }
    const indexed = indexCommentLines(parsePatch(file.patch));
    for (const group of groups) {
      const first = indexed
        .flatMap((line) => (line.RIGHT ? [line.RIGHT] : []))
        .find((line) => line.line === group.first + 1);
      const last = indexed
        .flatMap((line) => (line.RIGHT ? [line.RIGHT] : []))
        .find((line) => line.line === group.last + 1);
      if (!first || !last)
        throw new Error(
          "This fix touches lines outside the original diff. Preview it or select a fix confined to reviewable lines.",
        );
      const range = commentRange(indexed, first, last);
      if (!range.range) throw new Error(range.error);
      const blockStart = starts[group.first];
      const blockEnd = starts[group.last + 1] ?? content.length;
      let replacement = content.slice(blockStart, blockEnd);
      for (const { edit, start, end } of [...group.edits].reverse())
        replacement =
          replacement.slice(0, start - blockStart) +
          edit.newText +
          replacement.slice(end - blockStart);
      // A whole-line deletion differs from a replacement containing one blank line.
      const body =
        replacement === ""
          ? "```suggestion\n```"
          : insertSuggestion(
              "",
              replacement.endsWith("\n")
                ? replacement.slice(0, -1)
                : replacement,
              0,
              0,
            ).body;
      if (new TextEncoder().encode(body).length > 65536)
        throw new Error("This suggestion exceeds GitHub's comment size limit.");
      result.push({ path, ...rangeFields(range.range), body });
    }
  }
  return result;
}
