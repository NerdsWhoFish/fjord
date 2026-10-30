import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { linePosition, parsePatch, tokenize } from "./diff";
import { SyntaxText } from "./SyntaxText";
import { useSyntax } from "./useSyntax";
import type { HighlightSpan, SyntaxRequest } from "./syntax-types";
import type {
  ChangedFile,
  Method,
  PullRequest,
  Selection,
  SourceView,
} from "./types";
import { pageRows, sourceWindow } from "./window";
import { ContextActions } from "./ContextActions";
import type { ActionAnchor } from "./ContextActions";
import { commentRange, indexCommentLines, rangeFields } from "./comment-range";
import type { CommentRange } from "./comment-range";
import { useCommentRange } from "./useCommentRange";
import "./code-actions.css";

interface Props {
  pr: PullRequest;
  file: ChangedFile;
  selection?: Selection;
  source?: SourceView;
  busy: boolean;
  method: Method;
  onMethod: (method: Method) => void;
  onSelect: (selection: Selection) => void;
  onNavigate: (selection?: Selection, method?: Method) => void;
  onSource: (side: "LEFT" | "RIGHT") => void;
  onCloseSource: () => void;
  onComment: (selection: Selection) => void;
  onAsk: () => void;
}

function Tokens({
  code,
  spans,
  onIntent,
  onLeave,
}: {
  code: string;
  spans?: HighlightSpan[];
  onIntent: (
    element: HTMLElement,
    word: string,
    column: number,
    intent: "hover" | "focus" | "activate",
  ) => void;
  onLeave: () => void;
}) {
  if (code.length > 4096)
    return (
      <span>
        {code.slice(0, 4096)}
        <span className="long-line-notice">
          {" "}
          [Long line: showing 4,096 of {code.length.toLocaleString()}{" "}
          characters. Open this file on GitHub for the full line.]
        </span>
      </span>
    );
  const tokens = tokenize(code);
  if (tokens.length > 128)
    return (
      <span>
        {code}
        <span className="long-line-notice">
          {" "}
          [Symbol navigation is disabled for this unusually dense line.]
        </span>
      </span>
    );
  return (
    <>
      {tokens.map((token, i) =>
        token.symbol ? (
          <button
            type="button"
            className="code-token"
            key={i}
            onPointerEnter={(event) => {
              if (event.pointerType === "mouse")
                onIntent(
                  event.currentTarget,
                  token.text,
                  token.column,
                  "hover",
                );
            }}
            onPointerLeave={onLeave}
            onFocus={(event) => {
              if (event.currentTarget.matches(":focus-visible"))
                onIntent(
                  event.currentTarget,
                  token.text,
                  token.column,
                  "focus",
                );
            }}
            onBlur={(event) => {
              if (
                !(event.relatedTarget instanceof Element) ||
                !event.relatedTarget.closest(".code-context-actions")
              )
                onLeave();
            }}
            onClick={(event) =>
              onIntent(
                event.currentTarget,
                token.text,
                token.column,
                "activate",
              )
            }
            onKeyDown={(event) => {
              if (["ArrowDown", "Enter", " "].includes(event.key)) {
                event.preventDefault();
                onIntent(
                  event.currentTarget,
                  token.text,
                  token.column,
                  "activate",
                );
              }
            }}
            aria-label={`Inspect ${token.text}`}
            aria-haspopup="dialog"
          >
            <SyntaxText text={token.text} start={token.column} spans={spans} />
          </button>
        ) : (
          <SyntaxText
            key={i}
            text={token.text}
            start={token.column}
            spans={spans}
          />
        ),
      )}
    </>
  );
}

export function CodePanel({
  pr,
  file,
  source,
  busy,
  onMethod,
  onSelect,
  onNavigate,
  onSource,
  onCloseSource,
  onComment,
  onAsk,
}: Props) {
  const sourcePanel = useRef<HTMLDivElement>(null);
  const diffPanel = useRef<HTMLDivElement>(null);
  const [diffPage, setDiffPage] = useState(0);
  const [sourceStart, setSourceStart] = useState(0);
  const [actions, setActions] = useState<ActionAnchor>();
  const actionRef = useRef<ActionAnchor | undefined>(undefined);
  const openTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(
    undefined,
  );
  const suppressFocus = useRef(false);
  const sourceLines = useMemo(
    () => source?.content.split("\n") || [],
    [source?.content],
  );
  const lines = useMemo(() => parsePatch(file.patch || ""), [file.patch]);
  const indexed = useMemo(() => indexCommentLines(lines), [lines]);
  const cancelTimers = useCallback(() => {
    clearTimeout(openTimer.current);
    clearTimeout(closeTimer.current);
  }, []);
  const closeActions = useCallback(
    (restoreFocus = false) => {
      cancelTimers();
      const current = actionRef.current;
      actionRef.current = undefined;
      setActions(undefined);
      if (restoreFocus && current?.element.isConnected) {
        suppressFocus.current = true;
        current.element.focus({ preventScroll: true });
        setTimeout(() => {
          suppressFocus.current = false;
        }, 0);
      }
    },
    [cancelTimers],
  );
  const holdActions = useCallback(() => {
    clearTimeout(closeTimer.current);
  }, []);
  const leaveActions = useCallback(() => {
    clearTimeout(openTimer.current);
    if (
      actionRef.current?.activated ||
      actionRef.current?.pinned ||
      document.activeElement?.closest(".code-context-actions")
    )
      return;
    clearTimeout(closeTimer.current);
    closeTimer.current = setTimeout(() => closeActions(), 350);
  }, [closeActions]);
  function showActions(next: ActionAnchor) {
    cancelTimers();
    actionRef.current = next;
    setActions(next);
  }
  function rangeSelection(range: CommentRange): Selection {
    return {
      path:
        range.last.side === "LEFT" ? file.previousPath || file.path : file.path,
      ...rangeFields(range),
      character: 0,
      word: "",
      code: range.code,
      sha: range.last.side === "LEFT" ? pr.baseSHA : pr.headSHA,
    };
  }
  const range = useCommentRange(
    indexed,
    closeActions,
    (selected, element, activated) => {
      showActions({
        element,
        selection: rangeSelection(selected),
        commentable: true,
        activated,
        pinned: true,
      });
    },
    `${pr.owner}/${pr.repo}/${pr.baseSHA}/${pr.headSHA}/${file.path}`,
  );
  function symbolIntent(
    element: HTMLElement,
    selected: Selection,
    commentable: boolean,
    intent: "hover" | "focus" | "activate",
  ) {
    if (range.dragging() || (intent === "focus" && suppressFocus.current))
      return;
    cancelTimers();
    if (
      intent !== "activate" &&
      (actionRef.current?.activated || actionRef.current?.pinned)
    )
      return;
    const open = () => {
      if (!element.isConnected) return;
      if (intent === "activate") {
        range.clear();
        onSelect(selected);
      }
      showActions({
        element,
        selection: selected,
        commentable,
        activated: intent === "activate",
      });
    };
    if (intent === "hover") openTimer.current = setTimeout(open, 400);
    else open();
  }
  const diffRequest = useMemo<SyntaxRequest>(
    () => ({ kind: "diff", path: file.path, lines }),
    [file.path, lines],
  );
  const sourceRequest = useMemo<SyntaxRequest | undefined>(
    () =>
      source
        ? { kind: "source", path: source.path, code: source.content }
        : undefined,
    [source?.path, source?.content],
  );
  const diffSyntax = useSyntax(diffRequest);
  const sourceSyntax = useSyntax(sourceRequest);
  const diffWindow = pageRows(lines, diffPage);
  const visibleSource = sourceLines.slice(sourceStart, sourceStart + 200);
  useEffect(() => {
    setDiffPage(0);
    closeActions();
  }, [file.path, pr.baseSHA, pr.headSHA, closeActions]);
  useEffect(() => () => cancelTimers(), [cancelTimers]);
  useEffect(() => {
    setSourceStart(sourceWindow(sourceLines.length, source?.highlight).start);
  }, [source, sourceLines.length]);
  useEffect(() => {
    if (diffPanel.current) diffPanel.current.scrollTop = 0;
    closeActions();
  }, [diffPage, closeActions]);
  useEffect(() => {
    const scroller =
      sourcePanel.current?.querySelector<HTMLElement>(".source-scroll");
    if (scroller)
      scroller.scrollTop =
        source?.highlight !== undefined
          ? Math.max(
              0,
              (source.highlight - sourceStart) * 27 - scroller.clientHeight / 2,
            )
          : 0;
  }, [source, sourceStart]);
  return (
    <section
      className={`code-stage ${source ? "with-source" : ""}`}
      aria-label="Code review"
    >
      <div className="diff-pane">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">CHANGED FILE</span>
            <h2>{file.path}</h2>
          </div>
          <span className="file-status">{file.status}</span>
        </div>
        <div className="code-toolbar">
          <span>
            <b className="addition">+{file.additions}</b>{" "}
            <b className="deletion">-{file.deletions}</b>
          </span>
          <div className="button-group">
            <button
              disabled={busy || file.status === "added"}
              onClick={() => onSource("LEFT")}
            >
              Base source
            </button>
            <button
              disabled={busy || file.status === "removed"}
              onClick={() => onSource("RIGHT")}
            >
              Head source
            </button>
          </div>
        </div>
        {diffWindow.last > 0 && (
          <div className="page-controls">
            <button
              disabled={diffWindow.current === 0}
              onClick={() => setDiffPage(diffWindow.current - 1)}
            >
              Previous diff page
            </button>
            <span>
              Rows {diffWindow.start + 1}-{diffWindow.end} of {lines.length}
            </span>
            <button
              disabled={diffWindow.current === diffWindow.last}
              onClick={() => setDiffPage(diffWindow.current + 1)}
            >
              Next diff page
            </button>
          </div>
        )}
        <div
          className="diff-scroll"
          ref={diffPanel}
          tabIndex={0}
          role="region"
          aria-label={`Diff for ${file.path}`}
        >
          {lines.length ? (
            <div className="code-table">
              {diffWindow.rows.map((line, index) => {
                const rowIndex = diffWindow.start + index;
                const position = linePosition(line);
                const points = indexed[rowIndex];
                const selectionFor = (
                  word: string,
                  character: number,
                ): Selection => ({
                  path:
                    position?.side === "LEFT"
                      ? file.previousPath || file.path
                      : file.path,
                  line: position?.line || 1,
                  side: position?.side || "RIGHT",
                  character,
                  word,
                  code: line.text,
                  sha: position?.side === "LEFT" ? pr.baseSHA : pr.headSHA,
                });
                return (
                  <div
                    className={`code-row ${line.kind} ${range.selected(points.LEFT) || range.selected(points.RIGHT) ? "comment-range-selected" : ""}`}
                    key={rowIndex}
                    data-comment-row={rowIndex}
                  >
                    <span className="line-tools">
                      {position && (
                        <button
                          type="button"
                          className="line-comment-action"
                          title={`Comment on ${position.side === "LEFT" ? "base" : "head"} line ${position.line}`}
                          aria-label={`Comment on ${position.side === "LEFT" ? "base" : "head"} line ${position.line}`}
                          onClick={() => {
                            closeActions();
                            const point = points[position.side]!;
                            const selected = commentRange(
                              indexed,
                              point,
                              point,
                            );
                            if (selected.range)
                              onComment(rangeSelection(selected.range));
                          }}
                        >
                          +
                        </button>
                      )}
                    </span>
                    {(["LEFT", "RIGHT"] as const).map((side) => {
                      const point = points[side];
                      return point ? (
                        <button
                          key={side}
                          type="button"
                          className="line-number line-select"
                          data-comment-side={side}
                          title={`${side === "LEFT" ? "Base" : "Head"} line ${point.line}. Drag or Shift-click to select a range.`}
                          aria-label={`Select ${side === "LEFT" ? "base" : "head"} line ${point.line} for a comment`}
                          aria-pressed={range.selected(point)}
                          {...range.handlers(point)}
                        >
                          {point.line}
                        </button>
                      ) : (
                        <span className="line-number" key={side} />
                      );
                    })}
                    <span className="diff-marker" aria-hidden="true">
                      {line.kind === "add"
                        ? "+"
                        : line.kind === "delete"
                          ? "-"
                          : ""}
                    </span>
                    <code>
                      {position ? (
                        <Tokens
                          code={line.text}
                          spans={diffSyntax.lines[rowIndex]}
                          onIntent={(element, word, column, intent) =>
                            symbolIntent(
                              element,
                              selectionFor(word, column),
                              true,
                              intent,
                            )
                          }
                          onLeave={leaveActions}
                        />
                      ) : (
                        line.text
                      )}
                    </code>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="empty-message">
              <h3>GitHub did not provide a text diff</h3>
              <p>
                This file may be binary, too large, or have no textual changes.
                Open its source when available, or inspect it on GitHub.
              </p>
            </div>
          )}
        </div>
        {range.error && (
          <div className="range-feedback" role="status">
            {range.error}
          </div>
        )}
        <div className="code-footnote">
          <span className="live-dot" />
          Hover or select a symbol for code actions. Drag line numbers or
          Shift-click to comment on a range. Submit drafts from Review.
          {(diffSyntax.limited || sourceSyntax.limited) &&
            " Syntax highlighting unavailable for this content; showing plain text."}
        </div>
      </div>
      {source && (
        <div className="source-pane" ref={sourcePanel}>
          <div className="panel-heading">
            <div>
              <span className="eyebrow">
                {source.sha === pr.baseSHA ? "BASE" : "HEAD"} REVISION /{" "}
                {source.sha.slice(0, 7)}
              </span>
              <h2>{source.path}</h2>
            </div>
            <button
              className="icon-button"
              onClick={onCloseSource}
              aria-label="Close source pane"
            >
              x
            </button>
          </div>
          {sourceLines.length > 200 && (
            <div className="page-controls">
              <button
                disabled={sourceStart === 0}
                onClick={() => setSourceStart(Math.max(0, sourceStart - 200))}
              >
                Previous lines
              </button>
              <span>
                Lines {sourceStart + 1}-
                {Math.min(sourceLines.length, sourceStart + 200)} of{" "}
                {sourceLines.length}
              </span>
              <button
                disabled={sourceStart + 200 >= sourceLines.length}
                onClick={() => setSourceStart(sourceStart + 200)}
              >
                Next lines
              </button>
            </div>
          )}
          <div
            className="source-scroll"
            tabIndex={0}
            role="region"
            aria-label={`Source for ${source.path}`}
          >
            <div className="code-table">
              {visibleSource.map((code, offset) => {
                const i = sourceStart + offset;
                return (
                  <div
                    className={`code-row source-row ${source.highlight === i ? "target-line" : ""}`}
                    key={i}
                  >
                    <span className="line-number">{i + 1}</span>
                    <code>
                      <Tokens
                        code={code}
                        spans={sourceSyntax.lines[i]}
                        onIntent={(element, word, character, intent) =>
                          symbolIntent(
                            element,
                            {
                              path: source.path,
                              line: i + 1,
                              side:
                                source.sha === pr.baseSHA ? "LEFT" : "RIGHT",
                              sha: source.sha,
                              character,
                              word,
                              code,
                            },
                            false,
                            intent,
                          )
                        }
                        onLeave={leaveActions}
                      />
                    </code>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}
      {actions && (
        <ContextActions
          anchor={actions}
          busy={busy}
          onClose={closeActions}
          onEnter={holdActions}
          onLeave={leaveActions}
          onNavigate={(selected, action) => {
            closeActions();
            onSelect(selected);
            onMethod(action);
            onNavigate(selected, action);
          }}
          onComment={(selected) => {
            closeActions();
            onComment(selected);
          }}
          onAsk={(selected) => {
            closeActions();
            onSelect(selected);
            onAsk();
          }}
        />
      )}
    </section>
  );
}
