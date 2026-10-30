import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { createPortal } from "react-dom";
import { commentLineLabel } from "./comment-range";
import type {
  CodeLocation,
  Method,
  NavigationResult,
  QueryOptions,
  ReviewAction,
  ReviewItem,
  Selection,
} from "./types";
import { watchCapabilities } from "./navigation-capabilities";
import type {
  CapabilityLoader,
  CapabilityState,
} from "./navigation-capabilities";
import { availableActions, methodLabels as labels } from "./feature-methods";
import { FeatureResults } from "./FeatureResults";

export interface ActionAnchor {
  element: HTMLElement;
  selection: Selection;
  commentable: boolean;
  activated: boolean;
  pinned?: boolean;
  scope?: "file" | "symbol" | "diagnostic";
  diagnostics?: ReviewItem[];
}

interface Props {
  anchor: ActionAnchor;
  busy: boolean;
  onClose: (restoreFocus?: boolean) => void;
  onEnter: () => void;
  onLeave: () => void;
  onNavigate: (
    selection: Selection,
    method: Method,
    options?: QueryOptions,
  ) => Promise<NavigationResult | undefined>;
  onSuggest: (action: ReviewAction, selection: Selection) => Promise<void>;
  onClearAnnotations: () => void;
  onLocation: (location: CodeLocation) => void;
  onCapabilities: CapabilityLoader;
  onComment: (selection: Selection) => void;
}

export function ContextActions({
  anchor,
  busy,
  onClose,
  onEnter,
  onLeave,
  onNavigate,
  onSuggest,
  onClearAnnotations,
  onLocation,
  onCapabilities,
  onComment,
}: Props) {
  const panel = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ left: 8, top: 8, ready: false });
  const { selection } = anchor;
  const navigationEpoch = useRef(0);
  const currentSelection = useRef(selection);
  currentSelection.current = selection;
  const focusTarget = useRef<"back" | "query" | Method | undefined>(undefined);
  const queryID = useId();
  const [queryValue, setQueryValue] = useState("");
  const [navigation, setNavigation] = useState<{
    selection: Selection;
    method: Method;
    status: "query" | "loading" | "ready" | "error";
    result?: NavigationResult;
    error?: string;
  }>();
  const activeNavigation =
    navigation?.selection === selection ? navigation : undefined;
  useEffect(
    () => () => {
      navigationEpoch.current++;
    },
    [selection],
  );

  async function navigate(method: Method, options?: QueryOptions) {
    const epoch = ++navigationEpoch.current;
    onEnter();
    focusTarget.current = "back";
    setNavigation({ selection, method, status: "loading" });
    try {
      const next = await onNavigate(selection, method, options);
      if (
        navigationEpoch.current !== epoch ||
        currentSelection.current !== selection
      )
        return;
      setNavigation(
        next
          ? { selection, method, status: "ready", result: next }
          : {
              selection,
              method,
              status: "error",
              error: "Navigation did not complete. Go back to try again.",
            },
      );
    } catch (error) {
      if (
        navigationEpoch.current !== epoch ||
        currentSelection.current !== selection
      )
        return;
      setNavigation({
        selection,
        method,
        status: "error",
        error:
          error instanceof Error
            ? error.message
            : "Navigation is unavailable. Go back to try again.",
      });
    }
  }

  function chooseMethod(method: Method) {
    if (method === "workspaceSymbols" || method === "rename") {
      onEnter();
      setQueryValue(method === "rename" ? selection.word : "");
      focusTarget.current = "query";
      setNavigation({ selection, method, status: "query" });
    } else void navigate(method);
  }

  function backToActions() {
    navigationEpoch.current++;
    focusTarget.current = activeNavigation?.method;
    setNavigation(undefined);
  }

  useLayoutEffect(() => {
    const target = focusTarget.current;
    if (!target) return;
    const button =
      panel.current?.querySelector<HTMLElement>(
        `[data-navigation-focus="${target}"]:not(:disabled)`,
      ) ||
      panel.current?.querySelector<HTMLButtonElement>(
        ":scope > button:not(:disabled)",
      );
    button?.focus({ preventScroll: true });
    focusTarget.current = undefined;
  }, [activeNavigation]);
  const [attempt, setAttempt] = useState(0);
  const request = useMemo(
    () => ({ selection, load: onCapabilities, attempt }),
    [selection, onCapabilities, attempt],
  );
  const [result, setResult] = useState<{
    request: typeof request;
    state: CapabilityState;
  }>();
  // Hide the previous anchor's actions before the new request effect runs.
  const capabilityState: CapabilityState =
    result?.request === request ? result.state : { status: "loading" };
  const methods =
    selection.word && capabilityState.status === "ready"
      ? anchor.scope === "diagnostic"
        ? capabilityState.capabilities.methods.filter(
            (method) => method === "codeActions",
          )
        : availableActions(
            capabilityState.capabilities.methods,
            anchor.scope === "file",
          )
      : [];
  useEffect(
    () =>
      watchCapabilities(request.selection, request.load, (state) => {
        setResult({ request, state });
      }),
    [request],
  );
  useLayoutEffect(() => {
    function place() {
      if (!anchor.element.isConnected) {
        onClose();
        return;
      }
      const rect = anchor.element.getBoundingClientRect();
      if (
        rect.bottom < 0 ||
        rect.top > innerHeight ||
        rect.right < 0 ||
        rect.left > innerWidth
      ) {
        onClose();
        return;
      }
      let ancestor = anchor.element.parentElement;
      while (ancestor) {
        if (
          ancestor.matches(".diff-scroll, .source-scroll, .continuous-diffs")
        ) {
          const clip = ancestor.getBoundingClientRect();
          if (
            rect.bottom < clip.top ||
            rect.top > clip.bottom ||
            rect.right < clip.left ||
            rect.left > clip.right
          ) {
            onClose();
            return;
          }
        }
        ancestor = ancestor.parentElement;
      }
      const box = panel.current?.getBoundingClientRect();
      const width = box?.width || 270;
      const height = box?.height || 250;
      const top =
        rect.bottom + height + 8 <= innerHeight
          ? rect.bottom + 6
          : Math.max(8, rect.top - height - 6);
      setPosition({
        left: Math.max(8, Math.min(rect.left, innerWidth - width - 8)),
        top,
        ready: true,
      });
    }
    place();
    const observer =
      typeof ResizeObserver === "undefined"
        ? undefined
        : new ResizeObserver(place);
    if (panel.current) observer?.observe(panel.current);
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [anchor.element, anchor.selection, onClose, result, activeNavigation]);

  useEffect(() => {
    if (anchor.activated) {
      const first =
        panel.current?.querySelector<HTMLButtonElement>(
          ":scope > button:not(:disabled)",
        ) ||
        panel.current?.querySelector<HTMLButtonElement>(".context-dismiss");
      first?.focus({ preventScroll: true });
    }
  }, [anchor]);

  useEffect(() => {
    function outside(event: PointerEvent) {
      const target = event.target as Node;
      if (!panel.current?.contains(target) && !anchor.element.contains(target))
        onClose();
    }
    function escape(event: KeyboardEvent) {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose(
          anchor.activated || !!panel.current?.contains(document.activeElement),
        );
      }
    }
    document.addEventListener("pointerdown", outside);
    document.addEventListener("keydown", escape);
    return () => {
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("keydown", escape);
    };
  }, [anchor.element, onClose]);

  return createPortal(
    <div
      ref={panel}
      className={`code-context-actions ${activeNavigation ? "with-feature-results" : ""}`}
      role="dialog"
      aria-label={
        selection.word
          ? `Actions for ${selection.word}`
          : `Actions for lines ${commentLineLabel(selection.line, selection.startLine)}`
      }
      style={{
        left: position.left,
        top: position.top,
        visibility: position.ready ? "visible" : "hidden",
      }}
      onPointerEnter={onEnter}
      onPointerLeave={() => {
        if (!activeNavigation) onLeave();
      }}
      onFocus={onEnter}
      onBlur={(event) => {
        if (
          !activeNavigation &&
          !event.currentTarget.contains(event.relatedTarget)
        )
          onLeave();
      }}
    >
      <div className="context-action-heading">
        <div>
          <strong>
            {selection.word ||
              `Line${selection.startLine ? "s" : ""} ${commentLineLabel(selection.line, selection.startLine)}`}
          </strong>
          <span>
            {selection.side === "LEFT" ? "Base" : "Head"} / line{" "}
            {commentLineLabel(selection.line, selection.startLine)}
          </span>
        </div>
        <button
          type="button"
          className="context-dismiss"
          aria-label="Close code actions"
          onClick={() => onClose(true)}
        >
          x
        </button>
      </div>
      {!activeNavigation && anchor.diagnostics && (
        <ul className="diagnostic-messages">
          {anchor.diagnostics.map((item, index) => (
            <li key={index}>
              <strong className={`diagnostic-${item.kind}`}>{item.kind}</strong>
              <p>{item.label}</p>
              {item.detail && <small>{item.detail}</small>}
            </li>
          ))}
        </ul>
      )}
      {activeNavigation ? (
        <>
          <button
            type="button"
            data-navigation-focus="back"
            onClick={backToActions}
          >
            <span aria-hidden="true">&larr;</span>
            <span>Back to actions</span>
          </button>
          <div
            className="context-navigation"
            role="group"
            aria-label={`${labels[activeNavigation.method]} results`}
            aria-busy={activeNavigation.status === "loading"}
          >
            <p className="context-result-title">
              {labels[activeNavigation.method]}
            </p>
            {activeNavigation.status === "query" && (
              <form
                className="context-query"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (!queryValue.trim()) return;
                  void navigate(
                    activeNavigation.method,
                    activeNavigation.method === "rename"
                      ? { newName: queryValue.trim() }
                      : { query: queryValue.trim() },
                  );
                }}
              >
                <label htmlFor={queryID}>
                  {activeNavigation.method === "rename"
                    ? "New symbol name"
                    : "Search workspace symbols"}
                </label>
                <input
                  id={queryID}
                  data-navigation-focus="query"
                  value={queryValue}
                  onChange={(event) => setQueryValue(event.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                />
                {activeNavigation.method === "rename" && (
                  <p className="context-support-note">
                    Preview affected code without applying changes.
                  </p>
                )}
                <button type="submit" disabled={!queryValue.trim()}>
                  {activeNavigation.method === "rename"
                    ? "Preview rename"
                    : "Search symbols"}
                </button>
              </form>
            )}
            {activeNavigation.status === "loading" && (
              <p className="context-support-note" role="status">
                Loading results...
              </p>
            )}
            {activeNavigation.status === "error" && (
              <p className="context-support-note" role="alert">
                {activeNavigation.error}
              </p>
            )}
            {activeNavigation.status === "ready" && activeNavigation.result && (
              <FeatureResults
                result={activeNavigation.result}
                method={activeNavigation.method}
                selection={selection}
                onLocation={(location) => {
                  onClose();
                  onLocation(location);
                }}
                onSuggest={onSuggest}
                onClearAnnotations={onClearAnnotations}
              />
            )}
          </div>
        </>
      ) : (
        <>
          {methods.map((method) => (
            <button
              key={method}
              type="button"
              disabled={busy}
              data-navigation-focus={method}
              onClick={() => chooseMethod(method)}
            >
              <span>
                {anchor.scope === "diagnostic" && method === "codeActions"
                  ? "Show available fixes"
                  : labels[method]}
              </span>
              <span aria-hidden="true">&rarr;</span>
            </button>
          ))}
          {selection.word && capabilityState.status === "loading" && (
            <p className="context-support-note" role="status">
              Loading code navigation...
            </p>
          )}
          {selection.word && capabilityState.status === "error" && (
            <>
              <p className="context-support-note" role="status">
                Could not load code navigation. You can try again.
              </p>
              <button
                type="button"
                onClick={() => setAttempt((value) => value + 1)}
              >
                Retry code navigation
              </button>
            </>
          )}
          {selection.word &&
            capabilityState.status === "ready" &&
            (capabilityState.capabilities.message || !methods.length) && (
              <p className="context-support-note" role="status">
                {capabilityState.capabilities.message ||
                  (anchor.scope === "diagnostic"
                    ? "This language server does not offer code fixes here."
                    : "No code navigation actions are available for this file.")}
              </p>
            )}
        </>
      )}
      {anchor.commentable && (
        <button
          type="button"
          className="context-comment"
          onClick={() => onComment(selection)}
        >
          {selection.startLine
            ? "Comment on selected lines"
            : "Comment on this line"}
        </button>
      )}
    </div>,
    document.body,
  );
}
