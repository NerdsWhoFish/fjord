import type { Selection } from "./types";

export function suggestionUnavailable(
  selection?: Selection,
): string | undefined {
  if (!selection) return "Select head revision lines to suggest a replacement.";
  if (selection.side === "LEFT")
    return "Suggestions replace head revision lines. Select lines on the head side.";
  if (
    selection.code.endsWith(
      "\n[Preview limited to 32,768 characters. The comment still covers the selected lines.]",
    )
  )
    return "This code preview is truncated. Select fewer lines to insert a suggestion.";
}

export function insertSuggestion(
  body: string,
  code: string,
  selectionStart: number,
  selectionEnd: number,
): { body: string; codeStart: number; codeEnd: number } {
  const start = Math.max(
    0,
    Math.min(body.length, selectionStart, selectionEnd),
  );
  const end = Math.max(
    start,
    Math.min(body.length, Math.max(selectionStart, selectionEnd)),
  );
  const before = body.slice(0, start);
  const after = body.slice(end);
  const longestRun = (code.match(/`+/g) || []).reduce(
    (longest, run) => Math.max(longest, run.length),
    0,
  );
  const fence = "`".repeat(Math.max(3, longestRun + 1));
  const leading =
    !before || before.endsWith("\n\n")
      ? ""
      : before.endsWith("\n")
        ? "\n"
        : "\n\n";
  const trailing =
    !after || after.startsWith("\n\n")
      ? ""
      : after.startsWith("\n")
        ? "\n"
        : "\n\n";
  const opening = `${fence}suggestion\n`;
  const codeStart = before.length + leading.length + opening.length;
  return {
    body: `${before}${leading}${opening}${code}\n${fence}${trailing}${after}`,
    codeStart,
    codeEnd: codeStart + code.length,
  };
}
