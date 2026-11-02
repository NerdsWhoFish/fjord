import type { HighlightSpan } from "./syntax-types";
import "./syntax.css";

export function SyntaxText({
  text,
  start = 0,
  spans = [],
}: {
  text: string;
  start?: number;
  spans?: HighlightSpan[];
}) {
  const parts = [];
  const end = start + text.length;
  let cursor = start;
  for (const span of spans) {
    if (span.end <= start) continue;
    if (span.start >= end) break;
    const left = Math.max(start, span.start);
    const right = Math.min(end, span.end);
    if (left > cursor) parts.push(text.slice(cursor - start, left - start));
    parts.push(
      <span className={`syntax-${span.kind}`} key={left}>
        {text.slice(left - start, right - start)}
      </span>,
    );
    cursor = right;
  }
  if (cursor < end) parts.push(text.slice(cursor - start));
  return <>{parts}</>;
}
