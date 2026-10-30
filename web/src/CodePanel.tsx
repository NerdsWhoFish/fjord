import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import type { ReactNode } from "react";
import { linePosition, parsePatch, tokenize } from "./diff";
import { SyntaxText } from "./SyntaxText";
import { useSyntax } from "./useSyntax";
import type { HighlightSpan, SyntaxRequest } from "./syntax-types";
import type {
  ChangedFile,
  CodeLocation,
  Method,
  NavigationCapabilities,
  NavigationResult,
  PullRequest,
  Selection,
  Side,
  SourceView,
} from "./types";
import { sourceWindow } from "./window";
import { ReviewNavigation } from "./ReviewNavigation";
import { buildDiffContext } from "./diff-context";
import { ContextActions } from "./ContextActions";
import type { ActionAnchor } from "./ContextActions";
import { indexCommentLines, rangeFields } from "./comment-range";
import type { CommentRange } from "./comment-range";
import { useCommentRange } from "./useCommentRange";
import "./code-actions.css";
import "./review-flow.css";

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
  onCapabilities: (selection: Selection) => Promise<NavigationCapabilities>;
  onSource: (side: Side, file?: ChangedFile) => void;
  onLoadContent: (file: ChangedFile, side: Side) => Promise<string>;
  navigation?: NavigationResult;
  onLocation: (location: CodeLocation) => void;
  onDismissNavigation: () => void;
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

export function CodePanel(props: Props) {
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const target = Array.from(
      scroller.current?.querySelectorAll<HTMLElement>("[data-review-file]") ||
        [],
    ).find((element) => element.dataset.reviewFile === props.file.path);
    if (target && scroller.current)
      scroller.current.scrollTop +=
        target.getBoundingClientRect().top -
        scroller.current.getBoundingClientRect().top;
  }, [props.file.path]);
  return (
    <section
      className="continuous-review"
      aria-label="Pull request code review"
    >
      <ReviewNavigation
        result={props.navigation}
        method={props.method}
        busy={props.busy}
        onLocation={props.onLocation}
        onDismiss={props.onDismissNavigation}
      />
      <div className={`continuous-layout ${props.source ? "has-source" : ""}`}>
        <div
          className="continuous-diffs"
          ref={scroller}
          tabIndex={0}
          role="region"
          aria-label="All changed files"
        >
          {props.pr.files.map((file) => (
            <DeferredFile
              key={`${props.pr.baseSHA}/${props.pr.headSHA}/${file.path}`}
              file={file}
              active={file.path === props.file.path}
            >
              <FileCodePanel {...props} file={file} source={undefined} />
            </DeferredFile>
          ))}
        </div>
        {props.source && <FileCodePanel {...props} sourceOnly />}
      </div>
    </section>
  );
}

function DeferredFile({
  file,
  active,
  children,
}: {
  file: ChangedFile;
  active: boolean;
  children: ReactNode;
}) {
  const container = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(active);
  useEffect(() => {
    if (visible || !container.current) return;
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setVisible(true);
          observer.disconnect();
        }
      },
      { rootMargin: "600px" },
    );
    observer.observe(container.current);
    return () => observer.disconnect();
  }, [visible]);
  useEffect(() => {
    if (active) setVisible(true);
  }, [active]);
  return (
    <div ref={container} data-review-file={file.path}>
      {visible || active ? (
        children
      ) : (
        <div
          className="deferred-diff"
          style={{
            minHeight: Math.max(
              150,
              (file.patch?.split("\n").length || 1) * 25 + 180,
            ),
          }}
        >
          <h2>{file.path}</h2>
          <p>Diff loads as you scroll.</p>
        </div>
      )}
    </div>
  );
}

function FileCodePanel({
  pr,
  file,
  source,
  busy,
  onMethod,
  onSelect,
  onNavigate,
  onCapabilities,
  onSource,
  onLoadContent,
  onCloseSource,
  onComment,
  onAsk,
  sourceOnly = false,
}: Props & { sourceOnly?: boolean }) {
  const actionOwner = useId();
  const sourcePanel = useRef<HTMLDivElement>(null);
  const diffPanel = useRef<HTMLDivElement>(null);
  const [sourceStart, setSourceStart] = useState(0);
  const [contextContent, setContextContent] = useState<{
    base?: string;
    head?: string;
  }>();
  const [expanded, setExpanded] = useState<Record<string, number>>({});
  const [expanding, setExpanding] = useState<string>();
  const [contextError, setContextError] = useState("");
  const contextEpoch = useRef(0);
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
  const contextRows = useMemo(
    () => buildDiffContext(lines, contextContent, expanded),
    [lines, contextContent, expanded],
  );
  async function expandContext(key: string) {
    if (expanding) return;
    const requestEpoch = ++contextEpoch.current;
    setExpanding(key);
    setContextError("");
    try {
      if (!contextContent) {
        const [base, head] = await Promise.all([
          file.status === "added"
            ? Promise.resolve(undefined)
            : onLoadContent(file, "LEFT"),
          file.status === "removed"
            ? Promise.resolve(undefined)
            : onLoadContent(file, "RIGHT"),
        ]);
        if (contextEpoch.current !== requestEpoch) return;
        setContextContent({ base, head });
      }
      if (contextEpoch.current === requestEpoch)
        setExpanded((current) => ({
          ...current,
          [key]: (current[key] || 0) + 20,
        }));
    } catch {
      if (contextEpoch.current === requestEpoch)
        setContextError(
          "Could not load omitted context. Try expanding it again.",
        );
    } finally {
      if (contextEpoch.current === requestEpoch) setExpanding(undefined);
    }
  }
  useEffect(
    () => () => {
      contextEpoch.current++;
    },
    [],
  );
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
    window.dispatchEvent(
      new CustomEvent("fjord:code-actions", { detail: actionOwner }),
    );
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
  const diffRequest = useMemo<SyntaxRequest | undefined>(
    () =>
      sourceOnly
        ? undefined
        : {
            kind: "diff",
            path: file.path,
            lines: contextRows.map((row) =>
              row.kind === "gap" ? { kind: "hunk", text: "" } : row.line,
            ),
          },
    [file.path, contextRows, sourceOnly],
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
  const visibleSource = sourceLines.slice(sourceStart, sourceStart + 200);
  useEffect(() => {
    closeActions();
  }, [file.path, pr.baseSHA, pr.headSHA, closeActions]);
  useEffect(() => () => cancelTimers(), [cancelTimers]);
  useEffect(() => {
    function changed(event: Event) {
      if ((event as CustomEvent<string>).detail !== actionOwner) closeActions();
    }
    window.addEventListener("fjord:code-actions", changed);
    return () => window.removeEventListener("fjord:code-actions", changed);
  }, [actionOwner, closeActions]);
  useEffect(() => {
    setSourceStart(sourceWindow(sourceLines.length, source?.highlight).start);
    closeActions();
  }, [source, sourceLines.length, closeActions]);
  useEffect(() => closeActions(), [sourceStart, closeActions]);
  useEffect(() => {
    const scroller =
      sourcePanel.current?.querySelector<HTMLElement>(".source-scroll");
    if (!scroller) return;
    const target = scroller.querySelector<HTMLElement>(
      `[data-source-line="${source?.highlight}"]`,
    );
    if (target)
      scroller.scrollTop +=
        target.getBoundingClientRect().top -
        scroller.getBoundingClientRect().top -
        scroller.clientHeight / 2 +
        target.getBoundingClientRect().height / 2;
    else scroller.scrollTop = 0;
  }, [source, sourceStart]);
  return (
    <section
      className={`code-stage ${sourceOnly ? "shared-source" : "stream-file"}`}
      aria-label={
        sourceOnly
          ? `Source review for ${source?.path}`
          : `Diff review for ${file.path}`
      }
    >
      {!sourceOnly && (
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
                onClick={() => onSource("LEFT", file)}
              >
                Base source
              </button>
              <button
                disabled={busy || file.status === "removed"}
                onClick={() => onSource("RIGHT", file)}
              >
                Head source
              </button>
            </div>
          </div>
          <div
            className="diff-scroll"
            ref={diffPanel}
            tabIndex={0}
            role="region"
            aria-label={`Diff for ${file.path}`}
          >
            {lines.length ? (
              <div className="code-table">
                {contextRows.map((row, renderedIndex) => {
                  if (row.kind === "gap")
                    return (
                      <div className="diff-context-gap" key={row.key}>
                        <button
                          type="button"
                          disabled={!!expanding}
                          onClick={() => void expandContext(row.key)}
                        >
                          {expanding === row.key
                            ? "Loading context..."
                            : row.label}
                        </button>
                      </div>
                    );
                  const { line } = row;
                  const original = row.kind === "original";
                  const rowIndex = original ? row.index : undefined;
                  const position = linePosition(line);
                  const points =
                    rowIndex !== undefined ? indexed[rowIndex] : {};
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
                      className={`code-row ${line.kind} ${!original ? "expanded-context" : ""} ${range.selected(points.LEFT) || range.selected(points.RIGHT) ? "comment-range-selected" : ""}`}
                      key={original ? `original-${rowIndex}` : row.key}
                      data-comment-row={rowIndex}
                    >
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
                          <span className="line-number" key={side}>
                            {!original
                              ? side === "LEFT"
                                ? line.oldLine
                                : line.newLine
                              : ""}
                          </span>
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
                            spans={diffSyntax.lines[renderedIndex]}
                            onIntent={(element, word, column, intent) =>
                              symbolIntent(
                                element,
                                selectionFor(word, column),
                                original,
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
                  This file may be binary, too large, or have no textual
                  changes. Open its source when available, or inspect it on
                  GitHub.
                </p>
              </div>
            )}
          </div>
          {contextError && (
            <div className="range-feedback" role="status">
              {contextError}
            </div>
          )}
          {contextContent && (
            <div className="diff-context-note">
              Expanded context supports code navigation. Comments belong on
              lines in the original diff.
            </div>
          )}
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
      )}
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
                    data-source-line={i}
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
          onCapabilities={onCapabilities}
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
