import { useEffect, useState } from "react";
import type { ReviewItem, Selection } from "./types";
import {
  DiagnosticCache,
  diagnosticKey,
  diagnosticSeverity,
} from "./inline-diagnostics";
import type { DiagnosticState } from "./inline-diagnostics";
import "./inline-diagnostics.css";

export function useDiagnostics(
  cache: DiagnosticCache,
  selection: Selection | undefined,
) {
  const [attempt, setAttempt] = useState(0);
  const key = selection ? diagnosticKey(selection) : "";
  const [loaded, setLoaded] = useState<{
    key: string;
    cache: DiagnosticCache;
    state: DiagnosticState;
  }>();
  useEffect(() => {
    if (!selection) return;
    let active = true;
    setLoaded({ key, cache, state: { status: "loading" } });
    void cache.load(selection).then((state) => {
      if (active) setLoaded({ key, cache, state });
    });
    return () => {
      active = false;
    };
  }, [cache, key, attempt]);
  const state: DiagnosticState =
    loaded?.key === key && loaded.cache === cache
      ? loaded.state
      : { status: "loading" };
  return {
    state,
    retry: () => {
      if (selection) cache.invalidate(selection);
      setAttempt((value) => value + 1);
    },
  };
}

export function DiagnosticSummary({
  state,
  visible,
  onToggle,
  onRetry,
  revision,
}: {
  state: DiagnosticState;
  visible: boolean;
  onToggle: () => void;
  onRetry: () => void;
  revision: "Head" | "Base";
}) {
  if (state.status === "loading")
    return (
      <span className="diagnostic-summary" role="status">
        Loading {revision.toLowerCase()} diagnostics...
      </span>
    );
  if (state.status === "unsupported")
    return (
      <span className="diagnostic-summary" title={state.message}>
        Diagnostics unavailable
      </span>
    );
  if (state.status === "error" || state.pending)
    return (
      <span className="diagnostic-summary" title={state.message}>
        <span role="status">
          {state.status === "error"
            ? "Diagnostics failed"
            : "Diagnostics pending"}
        </span>
        <button type="button" onClick={onRetry}>
          Retry diagnostics
        </button>
      </span>
    );
  const errors = state.items.filter((item) => item.kind === "error").length;
  const warnings = state.items.filter((item) => item.kind === "warning").length;
  const others = state.items.length - errors - warnings;
  return (
    <span className="diagnostic-summary">
      <button
        type="button"
        aria-pressed={visible}
        title={state.message || `${revision} revision diagnostics`}
        onClick={onToggle}
      >
        {revision}: {errors} errors, {warnings} warnings
        {others ? `, ${others} notes` : ""}{" "}
        <span aria-hidden="true">{visible ? "Hide" : "Show"}</span>
      </button>
    </span>
  );
}

export function DiagnosticMarker({
  items,
  revision,
  onOpen,
}: {
  items: ReviewItem[];
  revision: "Head" | "Base";
  onOpen: (element: HTMLElement) => void;
}) {
  if (!items.length) return null;
  const severity = diagnosticSeverity(items);
  const label = `${revision} ${severity}: ${items.map((item) => item.label).join("; ")}`;
  return (
    <button
      type="button"
      className={`diagnostic-marker diagnostic-${severity}`}
      aria-label={label}
      title={label}
      aria-haspopup="dialog"
      onClick={(event) => onOpen(event.currentTarget)}
    >
      {severity === "error" || severity === "warning" ? "!" : "i"}
    </button>
  );
}
