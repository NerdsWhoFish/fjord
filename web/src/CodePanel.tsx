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
import { AnnotatedText } from "./AnnotatedText";
import { annotationsForLine, withDiagnostics } from "./code-annotations";
import type { LineAnnotations } from "./code-annotations";
import { DiagnosticCache, diagnosticsForLine } from "./inline-diagnostics";
import {
  DiagnosticMarker,
  DiagnosticSummary,
  useDiagnostics,
} from "./InlineDiagnostics";
import { useSyntax } from "./useSyntax";
import type { HighlightSpan, SyntaxRequest } from "./syntax-types";
import type {
  ChangedFile,
  CodeAnnotations,
  CodeLocation,
  Method,
  NavigationCapabilities,
  NavigationResult,
  PullRequest,
  QueryOptions,
  ReviewAction,
  ReviewItem,
  Selection,
  Side,
  SourceView,
  ThreadComment,
  DraftComment,
  FileDiff,
} from "./types";
import { sourceWindow } from "./window";
import { buildDiffContext } from "./diff-context";
import type { DiffContextContent } from "./diff-context";
import { ContextActions } from "./ContextActions";
import type { ActionAnchor } from "./ContextActions";
import { indexCommentLines, rangeFields } from "./comment-range";
import type { CommentRange } from "./comment-range";
import { useCommentRange } from "./useCommentRange";
import "./code-actions.css";
import "./review-flow.css";
import { diffLayout, reviewThreads } from "./diff-layout";
import {
  InlineDraftComments,
  InlineReviewThreads,
} from "./InlineReviewThreads";
import "./diff-layout.css";

interface Props {
  diffMode?: "unified" | "split";
  threads?: ThreadComment[];
  onReply?: (commentId: number, body: string) => Promise<void>;
  drafts?: DraftComment[];
  onReviewDrafts?: () => void;
  pr: PullRequest;
  file: ChangedFile;
  scrollTarget: { path: string };
  onActiveFile: (path: string) => void;
  viewed: Set<string>;
  onViewed: (path: string, checked: boolean) => void;
  selection?: Selection;
  source?: SourceView;
  busy: boolean;
  method: Method;
  onMethod: (method: Method) => void;
  onSelect: (selection: Selection) => void;
  onNavigate: (
    selection?: Selection,
    method?: Method,
    options?: QueryOptions,
  ) => Promise<NavigationResult | undefined>;
  onSuggest: (action: ReviewAction, selection: Selection) => Promise<void>;
  annotations?: CodeAnnotations;
  onClearAnnotations: () => void;
  onCapabilities: (selection: Selection) => Promise<NavigationCapabilities>;
  onDiagnostics: (selection: Selection) => Promise<NavigationResult>;
  onSource: (side: Side, file?: ChangedFile) => void;
  onLoadContent: (file: ChangedFile, side: Side) => Promise<string>;
  onLoadDiff?: (file: ChangedFile) => Promise<FileDiff>;
  onLocation: (location: CodeLocation) => void;
  onCloseSource: () => void;
  onComment: (selection: Selection) => void;
}

function Tokens({
  code,
  spans,
  annotations,
  onIntent,
  onLeave,
}: {
  code: string;
  spans?: HighlightSpan[];
  annotations?: LineAnnotations;
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
        <AnnotatedText
          text={code.slice(0, 4096)}
          spans={spans}
          annotations={annotations}
          includeEnd
        />
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
        <AnnotatedText
          text={code}
          spans={spans}
          annotations={annotations}
          includeEnd
        />
        <span className="long-line-notice">
          {" "}
          [Symbol navigation is disabled for this unusually dense line.]
        </span>
      </span>
    );
  return (
    <>
      {!tokens.length && (
        <AnnotatedText
          text=""
          spans={spans}
          annotations={annotations}
          includeEnd
        />
      )}
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
            <AnnotatedText
              text={token.text}
              start={token.column}
              spans={spans}
              annotations={annotations}
              includeEnd={i === tokens.length - 1}
            />
          </button>
        ) : (
          <AnnotatedText
            key={i}
            text={token.text}
            start={token.column}
            spans={spans}
            annotations={annotations}
            includeEnd={i === tokens.length - 1}
          />
        ),
      )}
    </>
  );
}

export function CodePanel(props: Props) {
  const diagnosticScope = `${props.pr.owner}/${props.pr.repo}/${props.pr.number}/${props.pr.baseSHA}/${props.pr.headSHA}`;
  const loaders = useRef({ props, scope: diagnosticScope });
  loaders.current = { props, scope: diagnosticScope };
  const diagnostics = useMemo(
    () =>
      new DiagnosticCache(
        (selection) => {
          if (loaders.current.scope !== diagnosticScope)
            return Promise.reject(new Error("The pull request changed."));
          return loaders.current.props.onCapabilities(selection);
        },
        (selection) => {
          if (loaders.current.scope !== diagnosticScope)
            return Promise.reject(new Error("The pull request changed."));
          return loaders.current.props.onDiagnostics(selection);
        },
      ),
    [diagnosticScope],
  );
  const scroller = useRef<HTMLDivElement>(null);
  const onActiveFile = useRef(props.onActiveFile);
  onActiveFile.current = props.onActiveFile;
  useEffect(() => {
    let frame = 0;
    function align() {
      const container = scroller.current;
      if (!container) return;
      const target = Array.from(
        container.querySelectorAll<HTMLElement>("[data-review-file]"),
      ).find(
        (element) => element.dataset.reviewFile === props.scrollTarget.path,
      );
      if (target)
        container.scrollTop +=
          target.getBoundingClientRect().top -
          container.getBoundingClientRect().top;
    }
    align();
    // Nearby placeholders can mount after the explicit jump changes visibility.
    frame = requestAnimationFrame(() => {
      align();
      frame = requestAnimationFrame(align);
    });
    const container = scroller.current;
    function cancelAlignment() {
      cancelAnimationFrame(frame);
    }
    container?.addEventListener("wheel", cancelAlignment, { passive: true });
    container?.addEventListener("pointerdown", cancelAlignment);
    container?.addEventListener("keydown", cancelAlignment);
    return () => {
      cancelAlignment();
      container?.removeEventListener("wheel", cancelAlignment);
      container?.removeEventListener("pointerdown", cancelAlignment);
      container?.removeEventListener("keydown", cancelAlignment);
    };
  }, [props.scrollTarget]);
  useEffect(() => {
    const container = scroller.current;
    if (!container) return;
    const files = Array.from(
      container.querySelectorAll<HTMLElement>("[data-review-file]"),
    );
    let frame = 0;
    let lastPath: string | undefined;
    function updateActive() {
      frame = 0;
      if (!container || !files.length || !container.clientHeight) return;
      const top = container.getBoundingClientRect().top + 8;
      let active = files[0];
      for (const file of files) {
        if (file.getBoundingClientRect().top > top) break;
        active = file;
      }
      if (
        container.scrollTop > 0 &&
        container.scrollTop + container.clientHeight >=
          container.scrollHeight - 1
      )
        active = files[files.length - 1];
      const path = active.dataset.reviewFile;
      if (path && path !== lastPath) {
        lastPath = path;
        onActiveFile.current(path);
      }
    }
    function schedule() {
      if (!frame) frame = requestAnimationFrame(updateActive);
    }
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(schedule);
    observer?.observe(container);
    files.forEach((file) => observer?.observe(file));
    container.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    schedule();
    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      container.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
    };
  }, [props.pr.files]);
  return (
    <section
      className="continuous-review"
      aria-label="Pull request code review"
    >
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
              <FileCodePanel
                {...props}
                file={file}
                source={undefined}
                diagnosticCache={diagnostics}
              />
            </DeferredFile>
          ))}
        </div>
        {props.source && (
          <FileCodePanel {...props} sourceOnly diagnosticCache={diagnostics} />
        )}
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
  diffMode = "unified",
  threads = [],
  onReply,
  drafts = [],
  onReviewDrafts,
  pr,
  file,
  source,
  busy,
  onMethod,
  onSelect,
  onNavigate,
  onSuggest,
  annotations,
  onClearAnnotations,
  onLocation,
  onCapabilities,
  onSource,
  onLoadContent,
  onLoadDiff,
  onCloseSource,
  onComment,
  viewed,
  onViewed,
  sourceOnly = false,
  diagnosticCache,
}: Props & { sourceOnly?: boolean; diagnosticCache: DiagnosticCache }) {
  const diagnosticSelection = useMemo<Selection>(() => {
    const base = sourceOnly
      ? source?.sha === pr.baseSHA
      : file.status === "removed";
    return {
      path: sourceOnly
        ? source!.path
        : base
          ? file.previousPath || file.path
          : file.path,
      sha: sourceOnly ? source!.sha : base ? pr.baseSHA : pr.headSHA,
      side: base ? "LEFT" : "RIGHT",
      line: 1,
      character: 0,
      word: sourceOnly ? source!.path : file.path,
      code: "",
    };
  }, [
    sourceOnly,
    source?.path,
    source?.sha,
    pr.baseSHA,
    pr.headSHA,
    file.path,
    file.previousPath,
    file.status,
  ]);
  const diagnostics = useDiagnostics(diagnosticCache, diagnosticSelection);
  const [showDiagnostics, setShowDiagnostics] = useState(true);
  const diagnosticItems =
    showDiagnostics && diagnostics.state.status === "ready"
      ? diagnostics.state.items
      : [];
  const diagnosticRevision =
    diagnosticSelection.side === "LEFT" ? "Base" : "Head";
  const diagnosticSummary = (
    <DiagnosticSummary
      state={diagnostics.state}
      visible={showDiagnostics}
      revision={diagnosticRevision}
      onToggle={() => setShowDiagnostics((value) => !value)}
      onRetry={diagnostics.retry}
    />
  );
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
  const [checkingContext, setCheckingContext] = useState(false);
  const contextRequest = useRef<Promise<DiffContextContent> | undefined>(
    undefined,
  );
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
  const [fallback, setFallback] = useState<FileDiff>();
  const [fallbackLoading, setFallbackLoading] = useState(
    !sourceOnly && !file.patch && !!onLoadDiff,
  );
  const [fallbackAttempt, setFallbackAttempt] = useState(0);
  const fallbackLoader = useRef(onLoadDiff);
  fallbackLoader.current = onLoadDiff;
  useEffect(() => {
    setFallback(undefined);
    if (sourceOnly || file.patch || !fallbackLoader.current) {
      setFallbackLoading(false);
      return;
    }
    let active = true;
    setFallbackLoading(true);
    void fallbackLoader
      .current(file)
      .then((result) => {
        if (active) setFallback(result);
      })
      .catch(() => {
        if (active)
          setFallback({
            patch: "",
            status: "unavailable",
            reason:
              "Could not load the pinned file content. Retry or inspect it on GitHub.",
          });
      })
      .finally(() => {
        if (active) setFallbackLoading(false);
      });
    return () => {
      active = false;
    };
  }, [
    sourceOnly,
    file.path,
    file.previousPath,
    file.patch,
    file.status,
    pr.baseSHA,
    pr.headSHA,
    fallbackAttempt,
  ]);
  const patch = file.patch || fallback?.patch || "";
  const commentable = !!file.patch && file.reviewable !== false;
  const lines = useMemo(() => parsePatch(patch), [patch]);
  const indexed = useMemo(
    () => indexCommentLines(commentable ? lines : []),
    [lines, commentable],
  );
  const contextRows = useMemo(
    () => buildDiffContext(lines, contextContent, expanded),
    [lines, contextContent, expanded],
  );
  const split = diffMode === "split";
  const layout = useMemo(
    () => diffLayout(contextRows, split),
    [contextRows, split],
  );
  const fileThreads = useMemo(
    () =>
      reviewThreads(threads).filter(
        ({ root }) =>
          root.path === file.path ||
          (root.side === "LEFT" && root.path === file.previousPath),
      ),
    [threads, file.path, file.previousPath],
  );
  const loadContext = useCallback(() => {
    if (contextRequest.current) return contextRequest.current;
    const request = Promise.all([
      file.status === "added"
        ? Promise.resolve("")
        : onLoadContent(file, "LEFT"),
      file.status === "removed"
        ? Promise.resolve("")
        : onLoadContent(file, "RIGHT"),
    ])
      .then(([base, head]) => ({ base, head }))
      .catch((error) => {
        contextRequest.current = undefined;
        throw error;
      });
    contextRequest.current = request;
    return request;
  }, [file, onLoadContent]);
  const [contextAttempt, setContextAttempt] = useState(0);
  useEffect(() => {
    if (
      sourceOnly ||
      !commentable ||
      file.status === "added" ||
      file.status === "removed" ||
      !lines.some((line) => line.kind === "hunk")
    )
      return;
    let active = true;
    setCheckingContext(true);
    setContextError("");
    void loadContext()
      .then((content) => {
        if (active) setContextContent(content);
      })
      .catch(() => {
        if (active) setContextError("Could not check omitted context.");
      })
      .finally(() => {
        if (active) setCheckingContext(false);
      });
    return () => {
      active = false;
    };
  }, [
    sourceOnly,
    commentable,
    file.status,
    lines,
    loadContext,
    contextAttempt,
  ]);
  async function expandContext(key: string) {
    if (expanding) return;
    const requestEpoch = ++contextEpoch.current;
    setExpanding(key);
    setContextError("");
    try {
      if (!contextContent) {
        const content = await loadContext();
        if (contextEpoch.current !== requestEpoch) return;
        setContextContent(content);
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
  function diagnosticActions(
    element: HTMLElement,
    items: ReviewItem[],
    code: string,
  ) {
    const location = items[0]?.location;
    if (!location) return;
    showActions({
      element,
      selection: {
        ...diagnosticSelection,
        line: location.line + 1,
        character: location.character,
        word: "Diagnostics",
        code,
      },
      activated: true,
      pinned: true,
      commentable: false,
      scope: "diagnostic",
      diagnostics: items,
    });
  }
  function fileTools(element: HTMLElement, path: string, sha: string) {
    showActions({
      element,
      selection: {
        path,
        sha,
        side: sha === pr.baseSHA ? "LEFT" : "RIGHT",
        line: 1,
        character: 0,
        word: path,
        code: "",
      },
      activated: true,
      pinned: true,
      commentable: false,
      scope: "file",
    });
  }
  const diffAnnotations =
    annotations &&
    ((annotations.sha === pr.headSHA && annotations.path === file.path) ||
      (annotations.sha === pr.baseSHA &&
        annotations.path === (file.previousPath || file.path)));
  const sourceAnnotations =
    source &&
    annotations?.path === source.path &&
    annotations.sha === source.sha;
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
            <div className="file-review-controls">
              {diagnosticSummary}
              <button
                type="button"
                aria-label={`Code tools for ${file.path}`}
                onClick={(event) =>
                  fileTools(
                    event.currentTarget,
                    file.status === "removed"
                      ? file.previousPath || file.path
                      : file.path,
                    file.status === "removed" ? pr.baseSHA : pr.headSHA,
                  )
                }
              >
                Code tools
              </button>
              <span className="file-status">{file.status}</span>
              <label className="viewed-control">
                <input
                  type="checkbox"
                  aria-label={`Mark ${file.path} as viewed`}
                  checked={viewed.has(file.path)}
                  onChange={(event) =>
                    onViewed(file.path, event.target.checked)
                  }
                />
                Viewed
              </label>
            </div>
          </div>
          <div className="code-toolbar">
            <span>
              <b className="addition">+{file.additions}</b>{" "}
              <b className="deletion">-{file.deletions}</b>
            </span>
            <div className="button-group">
              {diffAnnotations && (
                <button type="button" onClick={onClearAnnotations}>
                  Clear annotations
                </button>
              )}
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
          {fallback?.patch && (
            <p className="diff-context-note" role="status">
              {fallback.reason}
            </p>
          )}
          <div
            className="diff-scroll"
            ref={diffPanel}
            tabIndex={0}
            role="region"
            aria-label={`Diff for ${file.path}`}
          >
            {lines.length ? (
              <div className={`code-table ${split ? "split-diff" : ""}`}>
                {split && (
                  <>
                    <div className="split-diff-label">Base</div>
                    <div className="split-diff-label">Head</div>
                  </>
                )}
                {layout.map(
                  ({ row, index: renderedIndex, side: cellSide, track }) => {
                    const placement = split
                      ? {
                          gridRow: track + 1,
                          gridColumn:
                            cellSide === "LEFT"
                              ? "1"
                              : cellSide === "RIGHT"
                                ? "2"
                                : "1 / -1",
                        }
                      : undefined;
                    if (row.kind === "gap")
                      return (
                        <div
                          className="diff-context-gap"
                          key={row.key}
                          style={placement}
                        >
                          <button
                            type="button"
                            disabled={!!expanding}
                            onClick={() => void expandContext(row.key)}
                          >
                            <svg
                              className="context-direction"
                              aria-hidden="true"
                              viewBox="0 0 16 16"
                              width="16"
                              height="16"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="1.5"
                            >
                              <path d="M8 2v12" />
                              {row.key.startsWith("before:") ||
                              row.key.startsWith("between:") ? (
                                <path d="m4 6 4-4 4 4" />
                              ) : null}
                              {row.key.startsWith("after:") ||
                              row.key.startsWith("between:") ? (
                                <path d="m4 10 4 4 4-4" />
                              ) : null}
                            </svg>
                            {expanding === row.key
                              ? "Loading context..."
                              : row.label}
                          </button>
                        </div>
                      );
                    const { line } = row;
                    const original = row.kind === "original";
                    const rowIndex = original ? row.index : undefined;
                    const position = cellSide
                      ? {
                          side: cellSide,
                          line:
                            (cellSide === "LEFT"
                              ? line.oldLine
                              : line.newLine) ?? 1,
                        }
                      : linePosition(line);
                    const points =
                      (rowIndex !== undefined
                        ? indexed[rowIndex]
                        : undefined) || {};
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
                    const diagnosticLine =
                      cellSide && cellSide !== diagnosticSelection.side
                        ? 0
                        : ((diagnosticSelection.side === "LEFT"
                            ? line.oldLine
                            : line.newLine) ?? 0);
                    const rowDiagnostics =
                      diagnosticLine > 0
                        ? diagnosticsForLine(
                            diagnosticItems,
                            diagnosticSelection.path,
                            diagnosticLine - 1,
                          )
                        : [];
                    return (
                      <div
                        className={`diff-entry ${!cellSide ? "diff-full-row" : ""}`}
                        data-diff-side={cellSide}
                        style={placement}
                        key={`${original ? `original-${rowIndex}` : row.key}-${cellSide || "unified"}`}
                      >
                        <div
                          className={`code-row ${line.kind} ${!original ? "expanded-context" : ""} ${(cellSide ? range.selected(points[cellSide]) : range.selected(points.LEFT) || range.selected(points.RIGHT)) ? "comment-range-selected" : ""}`}
                          data-comment-row={rowIndex}
                        >
                          {(cellSide
                            ? [cellSide]
                            : (["LEFT", "RIGHT"] as const)
                          ).map((side) => {
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
                                {!original || !commentable
                                  ? side === "LEFT"
                                    ? line.oldLine
                                    : line.newLine
                                  : ""}
                              </span>
                            );
                          })}
                          <span className="diff-marker">
                            {rowDiagnostics.length ? (
                              <DiagnosticMarker
                                items={rowDiagnostics}
                                revision={diagnosticRevision}
                                onOpen={(element) =>
                                  diagnosticActions(
                                    element,
                                    rowDiagnostics,
                                    line.text,
                                  )
                                }
                              />
                            ) : (
                              <span aria-hidden="true">
                                {line.kind === "add"
                                  ? "+"
                                  : line.kind === "delete"
                                    ? "-"
                                    : ""}
                              </span>
                            )}
                          </span>
                          <code>
                            {position ? (
                              <Tokens
                                code={line.text}
                                spans={diffSyntax.lines[renderedIndex]}
                                annotations={withDiagnostics(
                                  annotationsForLine(
                                    annotations,
                                    (cellSide === "LEFT" ||
                                      (!cellSide &&
                                        annotations?.sha === pr.baseSHA)) &&
                                      line.oldLine !== undefined
                                      ? file.previousPath || file.path
                                      : file.path,
                                    (cellSide === "LEFT" ||
                                      (!cellSide &&
                                        annotations?.sha === pr.baseSHA)) &&
                                      line.oldLine !== undefined
                                      ? pr.baseSHA
                                      : pr.headSHA,
                                    ((cellSide === "LEFT" ||
                                      (!cellSide &&
                                        annotations?.sha === pr.baseSHA)) &&
                                    line.oldLine !== undefined
                                      ? line.oldLine
                                      : (line.newLine ?? -1)) - 1,
                                    line.text,
                                  ),
                                  rowDiagnostics,
                                  diagnosticLine - 1,
                                  line.text,
                                )}
                                onIntent={(element, word, column, intent) =>
                                  symbolIntent(
                                    element,
                                    selectionFor(word, column),
                                    original && commentable,
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
                        <InlineReviewThreads
                          threads={fileThreads.filter(
                            ({ root }) =>
                              (!cellSide || root.side === cellSide) &&
                              root.line ===
                                (root.side === "LEFT"
                                  ? line.oldLine
                                  : line.newLine),
                          )}
                          onReply={onReply}
                        />
                        <InlineDraftComments
                          drafts={drafts.filter(
                            (draft) =>
                              (draft.path === file.path ||
                                (draft.side === "LEFT" &&
                                  draft.path === file.previousPath)) &&
                              (!cellSide || draft.side === cellSide) &&
                              draft.line ===
                                (draft.side === "LEFT"
                                  ? line.oldLine
                                  : line.newLine),
                          )}
                          onReviewDrafts={onReviewDrafts}
                        />
                      </div>
                    );
                  },
                )}
              </div>
            ) : (
              <div className="empty-message">
                <h3>
                  {fallbackLoading
                    ? "Loading file content..."
                    : fallback?.status === "binary"
                      ? "Binary file"
                      : fallback?.status === "too_large"
                        ? "File exceeds the inline view limit"
                        : fallback?.status === "unchanged"
                          ? "No text content changed"
                          : "File content unavailable"}
                </h3>
                <p>
                  {fallbackLoading
                    ? "Fetching the exact revisions for a read-only view."
                    : fallback?.reason ||
                      "Open the source when available, or inspect this file on GitHub."}
                </p>
                {fallback?.status === "unavailable" && (
                  <button
                    type="button"
                    onClick={() => setFallbackAttempt((attempt) => attempt + 1)}
                  >
                    Retry file content
                  </button>
                )}
              </div>
            )}
          </div>
          {contextError && (
            <div className="range-feedback" role="status">
              {contextError}
              <button
                type="button"
                onClick={() => setContextAttempt((attempt) => attempt + 1)}
              >
                Retry context
              </button>
            </div>
          )}
          {checkingContext && (
            <div className="diff-context-note" role="status">
              Checking omitted context...
            </div>
          )}
          {Object.keys(expanded).length > 0 && (
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
            Shift-click to comment on a range. Review drafts using the controls
            below the PR title.
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
            <div className="source-review-controls">
              {diagnosticSummary}
              <button
                type="button"
                aria-label={`Code tools for ${source.path}`}
                onClick={(event) =>
                  fileTools(event.currentTarget, source.path, source.sha)
                }
              >
                Code tools
              </button>
              {sourceAnnotations && (
                <button type="button" onClick={onClearAnnotations}>
                  Clear annotations
                </button>
              )}
              <button
                className="icon-button"
                onClick={onCloseSource}
                aria-label="Close source pane"
              >
                x
              </button>
            </div>
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
                const rowDiagnostics = diagnosticsForLine(
                  diagnosticItems,
                  source.path,
                  i,
                );
                return (
                  <div
                    className={`code-row source-row ${source.highlight === i ? "target-line" : ""}`}
                    key={i}
                    data-source-line={i}
                  >
                    <span className="line-number">{i + 1}</span>
                    <span className="source-diagnostic-gutter">
                      <DiagnosticMarker
                        items={rowDiagnostics}
                        revision={diagnosticRevision}
                        onOpen={(element) =>
                          diagnosticActions(element, rowDiagnostics, code)
                        }
                      />
                    </span>
                    <code>
                      <Tokens
                        code={code}
                        spans={sourceSyntax.lines[i]}
                        annotations={withDiagnostics(
                          annotationsForLine(
                            annotations,
                            source.path,
                            source.sha,
                            i,
                            code,
                          ),
                          rowDiagnostics,
                          i,
                          code,
                        )}
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
          onNavigate={(selected, action, options) => {
            onMethod(action);
            return onNavigate(selected, action, options);
          }}
          onSuggest={onSuggest}
          onClearAnnotations={onClearAnnotations}
          onLocation={onLocation}
          onComment={(selected) => {
            closeActions();
            onComment(selected);
          }}
        />
      )}
    </section>
  );
}
