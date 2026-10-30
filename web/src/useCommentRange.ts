import { useEffect, useRef, useState } from "react";
import type { KeyboardEvent, MouseEvent, PointerEvent } from "react";
import { commentRange } from "./comment-range";
import type { CommentRange, IndexedLine, LinePoint } from "./comment-range";

interface Drag {
  anchor: LinePoint;
  pointer: number;
  range?: CommentRange;
  element: HTMLElement;
  valid: boolean;
}

export function useCommentRange(
  indexed: IndexedLine[],
  onStart: () => void,
  onFinish: (
    range: CommentRange,
    element: HTMLElement,
    activated: boolean,
  ) => void,
  resetKey: string,
) {
  const [range, setRange] = useState<CommentRange>();
  const [error, setError] = useState("");
  const anchor = useRef<LinePoint | undefined>(undefined);
  const drag = useRef<Drag | undefined>(undefined);
  const suppressClick = useRef(false);

  useEffect(() => {
    setRange(undefined);
    setError("");
    anchor.current = undefined;
    drag.current = undefined;
  }, [indexed, resetKey]);

  function choose(first: LinePoint, last: LinePoint) {
    const result = commentRange(indexed, first, last);
    setError(result.error || "");
    if (result.range) setRange(result.range);
    return result;
  }

  function pointAt(clientX: number, clientY: number) {
    const element = document.elementFromPoint(
      clientX,
      clientY,
    ) as HTMLElement | null;
    const row = element?.closest<HTMLElement>("[data-comment-row]");
    if (!row) return undefined;
    const button = element?.closest<HTMLElement>("[data-comment-side]");
    const side =
      button?.dataset.commentSide === "LEFT"
        ? "LEFT"
        : button?.dataset.commentSide === "RIGHT"
          ? "RIGHT"
          : drag.current?.anchor.side;
    const point = side && indexed[Number(row.dataset.commentRow)]?.[side];
    return point ? { point, element: button || row } : undefined;
  }

  function pointerDown(
    event: PointerEvent<HTMLButtonElement>,
    point: LinePoint,
  ) {
    if (event.button !== 0) return;
    event.preventDefault();
    onStart();
    event.currentTarget.focus({ preventScroll: true });
    suppressClick.current = false;
    const first = event.shiftKey && anchor.current ? anchor.current : point;
    const result = choose(first, point);
    anchor.current = first;
    drag.current = {
      anchor: first,
      pointer: event.pointerId,
      range: result.range,
      valid: !!result.range,
      element: event.currentTarget,
    };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function pointerMove(event: PointerEvent<HTMLButtonElement>) {
    if (!drag.current || drag.current.pointer !== event.pointerId) return;
    event.preventDefault();
    const target = pointAt(event.clientX, event.clientY);
    if (!target) return;
    const result = choose(drag.current.anchor, target.point);
    drag.current.valid = !!result.range;
    drag.current.range = result.range;
    drag.current.element = target.element;
  }

  function pointerUp(event: PointerEvent<HTMLButtonElement>) {
    const current = drag.current;
    if (!current || current.pointer !== event.pointerId) return;
    event.preventDefault();
    drag.current = undefined;
    suppressClick.current = true;
    if (event.currentTarget.hasPointerCapture(event.pointerId))
      event.currentTarget.releasePointerCapture(event.pointerId);
    if (current.valid && current.range)
      onFinish(current.range, current.element, false);
  }

  function click(event: MouseEvent<HTMLButtonElement>, point: LinePoint) {
    if (suppressClick.current) {
      suppressClick.current = false;
      return;
    }
    onStart();
    const first = event.shiftKey && anchor.current ? anchor.current : point;
    anchor.current = first;
    const result = choose(first, point);
    if (result.range)
      onFinish(result.range, event.currentTarget, event.detail === 0);
  }

  function keyDown(event: KeyboardEvent<HTMLButtonElement>, point: LinePoint) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onStart();
      if (
        !event.shiftKey &&
        range &&
        point.side === range.last.side &&
        point.hunk === range.last.hunk &&
        point.line >= range.first.line &&
        point.line <= range.last.line
      ) {
        onFinish(range, event.currentTarget, true);
        return;
      }
      const first = event.shiftKey && anchor.current ? anchor.current : point;
      anchor.current = first;
      const result = choose(first, point);
      if (result.range) onFinish(result.range, event.currentTarget, true);
      return;
    }
    if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
    event.preventDefault();
    const step = event.key === "ArrowUp" ? -1 : 1;
    for (
      let row = point.row + step;
      row >= 0 && row < indexed.length;
      row += step
    ) {
      const next = indexed[row][point.side];
      if (!next) continue;
      if (next.hunk !== point.hunk) return;
      const element = event.currentTarget
        .closest(".diff-scroll")
        ?.querySelector<HTMLElement>(
          `[data-comment-row="${row}"] [data-comment-side="${point.side}"]`,
        );
      if (!element) return;
      element.focus({ preventScroll: true });
      if (event.shiftKey) {
        const first = anchor.current || point;
        anchor.current = first;
        const result = choose(first, next);
        if (result.range) onFinish(result.range, element, false);
      }
      return;
    }
  }

  return {
    range,
    error,
    dragging: () => !!drag.current,
    clear: () => {
      setRange(undefined);
      setError("");
      anchor.current = undefined;
      drag.current = undefined;
    },
    selected: (point?: LinePoint) =>
      !!point &&
      !!range &&
      range.first.side === point.side &&
      range.first.hunk === point.hunk &&
      point.line >= range.first.line &&
      point.line <= range.last.line,
    handlers: (point: LinePoint) => ({
      onPointerDown: (event: PointerEvent<HTMLButtonElement>) =>
        pointerDown(event, point),
      onPointerMove: pointerMove,
      onPointerUp: pointerUp,
      onPointerCancel: () => {
        drag.current = undefined;
        setRange(undefined);
      },
      onClick: (event: MouseEvent<HTMLButtonElement>) => click(event, point),
      onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) =>
        keyDown(event, point),
    }),
  };
}
