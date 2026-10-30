import type { CodeAnnotations, InlayHint } from "./types";

export interface LineAnnotations {
  highlights: { start: number; end: number; kind: string }[];
  hints: { position: number; hint: InlayHint }[];
}

const empty: LineAnnotations = { highlights: [], hints: [] };

function boundary(code: string, position: number, end: boolean): number {
  const value = Math.max(0, Math.min(code.length, position));
  const current = code.charCodeAt(value);
  const previous = code.charCodeAt(value - 1);
  if (
    current >= 0xdc00 &&
    current <= 0xdfff &&
    previous >= 0xd800 &&
    previous <= 0xdbff
  )
    return value + (end ? 1 : -1);
  return value;
}

export function annotationsForLine(
  annotations: CodeAnnotations | undefined,
  path: string,
  sha: string,
  line: number,
  code: string,
): LineAnnotations {
  if (!annotations || annotations.path !== path || annotations.sha !== sha)
    return empty;
  const highlights = (annotations.highlights || []).flatMap(
    ({ location, kind }) => {
      if (
        location.path !== path ||
        location.line < 0 ||
        location.endLine < location.line ||
        location.character < 0 ||
        location.endCharacter < 0 ||
        line < location.line ||
        line > location.endLine ||
        ![
          location.line,
          location.endLine,
          location.character,
          location.endCharacter,
        ].every(Number.isInteger)
      )
        return [];
      const start = boundary(
        code,
        line === location.line ? location.character : 0,
        false,
      );
      const end = boundary(
        code,
        line === location.endLine ? location.endCharacter : code.length,
        true,
      );
      return end > start
        ? [
            {
              start,
              end,
              kind:
                kind === "write" ? "write" : kind === "read" ? "read" : "text",
            },
          ]
        : [];
    },
  );
  const hints = (annotations.hints || [])
    .flatMap((hint) => {
      const { location } = hint;
      if (
        location.path !== path ||
        location.line !== line ||
        !Number.isInteger(location.character) ||
        location.character < 0 ||
        location.character > code.length
      )
        return [];
      return [{ position: boundary(code, location.character, true), hint }];
    })
    .sort((a, b) => a.position - b.position);
  return { highlights, hints };
}

export type AnnotationPart =
  | { kind: "text"; text: string; start: number; highlight?: string }
  | { kind: "hint"; hint: InlayHint };

export function annotationParts(
  text: string,
  start: number,
  annotations: LineAnnotations,
  includeEnd = false,
): AnnotationPart[] {
  const end = start + text.length;
  const hints = annotations.hints.filter(
    ({ position }) =>
      position >= start && (position < end || (includeEnd && position === end)),
  );
  const boundaries = new Set([start, end]);
  annotations.highlights.forEach((range) => {
    if (range.start > start && range.start < end) boundaries.add(range.start);
    if (range.end > start && range.end < end) boundaries.add(range.end);
  });
  hints.forEach(({ position }) => boundaries.add(position));
  const points = [...boundaries].sort((a, b) => a - b);
  const parts: AnnotationPart[] = [];
  points.forEach((position, index) => {
    hints
      .filter((hint) => hint.position === position)
      .forEach(({ hint }) => parts.push({ kind: "hint", hint }));
    const next = points[index + 1];
    if (next === undefined || next <= position) return;
    const matches = annotations.highlights.filter(
      (range) => range.start <= position && range.end >= next,
    );
    const highlight =
      matches.find((range) => range.kind === "write") || matches[0];
    parts.push({
      kind: "text",
      text: text.slice(position - start, next - start),
      start: position,
      highlight: highlight?.kind,
    });
  });
  return parts;
}
