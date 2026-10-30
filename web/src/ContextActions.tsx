import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { commentLineLabel } from "./comment-range";
import type {
  CodeLocation,
  Method,
  NavigationResult,
  Selection,
} from "./types";
import { watchCapabilities } from "./navigation-capabilities";
import type {
  CapabilityLoader,
  CapabilityState,
} from "./navigation-capabilities";

export interface ActionAnchor {
  element: HTMLElement;
  selection: Selection;
  commentable: boolean;
  activated: boolean;
  pinned?: boolean;
}

const labels: Record<Method, string> = {
  definition: "Definition",
  implementation: "Implementation",
  references: "References",
  hover: "Type information",
};

interface Props {
  anchor: ActionAnchor;
  busy: boolean;
  onClose: (restoreFocus?: boolean) => void;
  onEnter: () => void;
  onLeave: () => void;
  onNavigate: (
    selection: Selection,
    method: Method,
  ) => Promise<NavigationResult | undefined>;
  onLocation: (location: CodeLocation) => void;
  onCapabilities: CapabilityLoader;
  onComment: (selection: Selection) => void;
  onAsk: (selection: Selection) => void;
}

export function ContextActions({
  anchor,
  busy,
  onClose,
  onEnter,
  onLeave,
  onNavigate,
  onLocation,
  onCapabilities,
  onComment,
  onAsk,
}: Props) {
  const panel = useRef<HTMLDivElement>(null);
  const [position, setPosition] = useState({ left: 8, top: 8, ready: false });
  const { selection } = anchor;
  const navigationEpoch = useRef(0);
  const currentSelection = useRef(selection);
  currentSelection.current = selection;
  const focusTarget = useRef<"back" | "go-to" | Method | undefined>(undefined);
  const [goToSelection, setGoToSelection] = useState<Selection>();
  const goToOpen = goToSelection === selection;
  const [navigation, setNavigation] = useState<{
    selection: Selection;
    method: Method;
    status: "loading" | "ready" | "error";
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

  async function navigate(method: Method) {
    const epoch = ++navigationEpoch.current;
    onEnter();
    focusTarget.current = "back";
    setNavigation({ selection, method, status: "loading" });
    try {
      const next = await onNavigate(selection, method);
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

  function backToActions() {
    navigationEpoch.current++;
    focusTarget.current = activeNavigation?.method;
    setNavigation(undefined);
  }

  useLayoutEffect(() => {
    const target = focusTarget.current;
    if (!target) return;
    const button =
      panel.current?.querySelector<HTMLButtonElement>(
        `[data-navigation-focus="${target}"]:not(:disabled)`,
      ) ||
      panel.current?.querySelector<HTMLButtonElement>(
        ":scope > button:not(:disabled)",
      );
    button?.focus({ preventScroll: true });
    focusTarget.current = undefined;
  }, [activeNavigation, goToOpen]);
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
      ? capabilityState.capabilities.methods
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
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [
    anchor.element,
    anchor.selection,
    onClose,
    result,
    activeNavigation,
    goToOpen,
  ]);

  useEffect(() => {
    if (anchor.activated)
      panel.current
        ?.querySelector<HTMLButtonElement>(":scope > button:not(:disabled)")
        ?.focus({ preventScroll: true });
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
      className="code-context-actions"
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
      {activeNavigation ? (
        <>
          <button
            type="button"
            data-navigation-focus="back"
            onClick={backToActions}
          >
            <span aria-hidden="true">&larr;</span>
            <span>{goToOpen ? "Back to navigation" : "Back to actions"}</span>
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
            {activeNavigation.status === "loading" && (
              <p className="context-support-note" role="status">
                Loading{" "}
                {activeNavigation.method === "hover"
                  ? "type information"
                  : "destinations"}
                ...
              </p>
            )}
            {activeNavigation.status === "error" && (
              <p className="context-support-note" role="alert">
                {activeNavigation.error}
              </p>
            )}
            {activeNavigation.status === "ready" && (
              <>
                {!!activeNavigation.result?.hover && (
                  <pre className="context-hover-result">
                    {activeNavigation.result.hover}
                  </pre>
                )}
                {!!activeNavigation.result?.locations?.length && (
                  <>
                    <p className="context-support-note" role="status">
                      {activeNavigation.result.locations.length}{" "}
                      {activeNavigation.method === "references"
                        ? activeNavigation.result.locations.length === 1
                          ? "reference"
                          : "references"
                        : activeNavigation.result.locations.length === 1
                          ? "destination"
                          : "destinations"}
                    </p>
                    <div
                      className="context-destinations"
                      role="group"
                      aria-label="Navigation destinations"
                    >
                      {activeNavigation.result.locations.map(
                        (location, index) => (
                          <button
                            type="button"
                            key={`${location.path}:${location.line}:${location.character}:${index}`}
                            onClick={() => {
                              onClose();
                              onLocation(location);
                            }}
                          >
                            <span>
                              {location.path}:{location.line + 1}
                            </span>
                            <span aria-hidden="true">&rarr;</span>
                          </button>
                        ),
                      )}
                    </div>
                  </>
                )}
                {!activeNavigation.result?.hover &&
                  !activeNavigation.result?.locations?.length && (
                    <p className="context-support-note" role="status">
                      {activeNavigation.result?.message ||
                        "No results for this symbol."}
                    </p>
                  )}
              </>
            )}
          </div>
        </>
      ) : (
        <>
          {goToOpen ? (
            <button
              type="button"
              onClick={() => {
                focusTarget.current = "go-to";
                setGoToSelection(undefined);
              }}
            >
              <span aria-hidden="true">&larr;</span>
              <span>Back to actions</span>
            </button>
          ) : methods.some((method) => method !== "hover") ? (
            <button
              type="button"
              data-navigation-focus="go-to"
              onClick={() => {
                onEnter();
                focusTarget.current = methods.find(
                  (method) => method !== "hover",
                );
                setGoToSelection(selection);
              }}
            >
              <span>Go to</span>
              <span aria-hidden="true">&rarr;</span>
            </button>
          ) : null}
          {methods
            .filter((method) =>
              goToOpen ? method !== "hover" : method === "hover",
            )
            .map((method) => (
              <button
                key={method}
                type="button"
                disabled={busy}
                data-navigation-focus={method}
                onClick={() => void navigate(method)}
              >
                <span>{labels[method]}</span>
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
                  "No code navigation actions are available for this file."}
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
      {selection.word && (
        <button type="button" onClick={() => onAsk(selection)}>
          Ask AI about selection
        </button>
      )}
    </div>,
    document.body,
  );
}
