import { SyntaxText } from "./SyntaxText";
import { annotationParts } from "./code-annotations";
import type { LineAnnotations } from "./code-annotations";
import type { HighlightSpan } from "./syntax-types";

export function AnnotatedText({
  text,
  start = 0,
  spans,
  annotations,
  includeEnd = false,
}: {
  text: string;
  start?: number;
  spans?: HighlightSpan[];
  annotations?: LineAnnotations;
  includeEnd?: boolean;
}) {
  if (
    !annotations ||
    (!annotations.highlights.length && !annotations.hints.length)
  )
    return <SyntaxText text={text} start={start} spans={spans} />;
  return (
    <>
      {annotationParts(text, start, annotations, includeEnd).map(
        (part, index) => {
          if (part.kind === "hint")
            return (
              <span
                key={index}
                className="code-inlay-hint"
                title={`Inlay hint: ${part.hint.label}`}
                aria-hidden="true"
              >
                {part.hint.paddingLeft ? " " : ""}
                {part.hint.label}
                {part.hint.paddingRight ? " " : ""}
              </span>
            );
          const content = (
            <SyntaxText text={part.text} start={part.start} spans={spans} />
          );
          return part.highlight ? (
            <mark
              key={index}
              className={`code-document-highlight highlight-${part.highlight}`}
            >
              {content}
            </mark>
          ) : (
            <span key={index}>{content}</span>
          );
        },
      )}
    </>
  );
}
