import { useEffect, useState } from "react";
import type {
  CodeLocation,
  NavigationResult,
  ReviewItem,
  Selection,
} from "./types";

export type InspectCode = (
  selected: Selection,
  method: "documentSymbols" | "workspaceSymbols" | "diagnostics",
  options?: { query?: string },
) => Promise<NavigationResult>;

export function activeOutlineLocation(
  items: ReviewItem[],
  path: string,
  line?: number,
): CodeLocation | undefined {
  if (line === undefined) return;
  const currentLine = line;
  let active: CodeLocation | undefined;
  function visit(entries: ReviewItem[]) {
    for (const item of entries) {
      const location = item.location;
      if (
        location?.path === path &&
        location.line <= currentLine &&
        (!active || location.line >= active.line)
      )
        active = location;
      if (item.children) visit(item.children);
    }
  }
  visit(items);
  return active;
}

function OutlineItems({
  items,
  active,
  onLocation,
}: {
  items: ReviewItem[];
  active?: CodeLocation;
  onLocation: (location: CodeLocation) => void;
}) {
  return (
    <ul className="outline-items">
      {items.map((item, index) => (
        <li key={index}>
          {item.location ? (
            <button
              className={item.location === active ? "active" : ""}
              aria-current={item.location === active ? "location" : undefined}
              onClick={() => onLocation(item.location!)}
            >
              <span className="outline-symbol-kind">
                {item.kind || "symbol"}
              </span>
              <span className="outline-symbol-name">{item.label}</span>
              <span className="outline-line">{item.location.line + 1}</span>
            </button>
          ) : (
            <span className="outline-group">{item.label}</span>
          )}
          {!!item.children?.length && (
            <details open>
              <summary>{item.children.length} nested symbols</summary>
              <OutlineItems
                items={item.children}
                active={active}
                onLocation={onLocation}
              />
            </details>
          )}
        </li>
      ))}
    </ul>
  );
}

export function OutlinePanel({
  selected,
  currentLine,
  inspect,
  onLocation,
}: {
  selected?: Selection;
  currentLine?: number;
  inspect: InspectCode;
  onLocation: (location: CodeLocation, selected: Selection) => void;
}) {
  const [state, setState] = useState<{
    key: string;
    result?: NavigationResult;
    error?: string;
  }>();
  const [attempt, setAttempt] = useState(0);
  const key = selected ? `${selected.sha}:${selected.path}` : "";
  useEffect(() => {
    let current = true;
    if (!selected) return;
    inspect(selected, "documentSymbols").then(
      (result) => {
        if (current) setState({ key, result });
      },
      (error) => {
        if (current)
          setState({
            key,
            error:
              error instanceof Error
                ? error.message
                : "Could not load the file outline.",
          });
      },
    );
    return () => {
      current = false;
    };
  }, [key, inspect, attempt]);
  const visible = state?.key === key ? state : undefined;
  const items = visible?.result?.items || [];
  const active = activeOutlineLocation(
    items,
    selected?.path || "",
    currentLine,
  );
  return (
    <section className="outline-panel" aria-label="File outline">
      <div className="section-title">
        <span className="eyebrow">FILE OUTLINE</span>
      </div>
      {selected ? (
        <>
          <p className="outline-path">{selected.path}</p>
          <p className="outline-revision">
            {selected.side === "LEFT" ? "Base" : "Head"} /{" "}
            {selected.sha.slice(0, 7)}
          </p>
          {!visible && (
            <p className="muted small" role="status">
              Loading file outline...
            </p>
          )}
          {visible?.error && (
            <div role="status">
              <p className="muted small">{visible.error}</p>
              <button
                onClick={() => {
                  setState(undefined);
                  setAttempt((value) => value + 1);
                }}
              >
                Retry outline
              </button>
            </div>
          )}
          {!!items.length && (
            <OutlineItems
              items={items}
              active={active}
              onLocation={(location) => onLocation(location, selected)}
            />
          )}
          {visible?.result?.message && (
            <p className="muted small">{visible.result.message}</p>
          )}
          {visible?.result && !items.length && !visible.result.message && (
            <p className="muted small">No symbols reported for this file.</p>
          )}
        </>
      ) : (
        <p className="muted small">Choose a file to explore its outline.</p>
      )}
    </section>
  );
}
