import { useEffect, useRef, useState } from "react";
import type {
  CodeLocation,
  Method,
  NavigationResult,
  ReviewAction,
  ReviewItem,
  Selection,
  TextEdit,
} from "./types";

function Locations({
  label,
  locations,
  onLocation,
}: {
  label: string;
  locations: CodeLocation[];
  onLocation: (location: CodeLocation) => void;
}) {
  return (
    <section className="context-location-group" aria-label={label}>
      <h3>
        {label} <span>({locations.length})</span>
      </h3>
      {locations.length ? (
        <div className="context-destinations">
          {locations.map((location, index) => (
            <button
              type="button"
              key={index}
              onClick={() => onLocation(location)}
            >
              <span>
                {location.path}:{location.line + 1}
              </span>
              <span aria-hidden="true">&rarr;</span>
            </button>
          ))}
        </div>
      ) : (
        <p className="context-support-note">
          No {label.toLowerCase()} returned.
        </p>
      )}
    </section>
  );
}

function Items({
  items,
  onLocation,
}: {
  items: ReviewItem[];
  onLocation: (location: CodeLocation) => void;
}) {
  return (
    <ul className="context-review-items">
      {items.map((item, index) => (
        <li key={index}>
          <div className="context-review-item">
            {item.kind && (
              <span className="context-item-kind">{item.kind}</span>
            )}
            {item.location ? (
              <button type="button" onClick={() => onLocation(item.location!)}>
                {item.label}
                <span className="context-item-location">
                  {item.location.path}:{item.location.line + 1}
                </span>
              </button>
            ) : (
              <strong>{item.label}</strong>
            )}
          </div>
          {item.detail && <p className="context-support-note">{item.detail}</p>}
          {!!item.children?.length && (
            <details>
              <summary>{item.children.length} nested items</summary>
              <Items items={item.children} onLocation={onLocation} />
            </details>
          )}
        </li>
      ))}
    </ul>
  );
}

function EditPreview({
  edits,
  onLocation,
}: {
  edits: TextEdit[];
  onLocation: (location: CodeLocation) => void;
}) {
  return (
    <div className="context-edit-previews">
      {edits.map((edit, index) => (
        <article key={index}>
          <button
            type="button"
            className="context-edit-location"
            onClick={() => onLocation(edit.location)}
          >
            {edit.location.path}:{edit.location.line + 1}-
            {edit.location.endLine + 1}
          </button>
          <p className="context-edit-label">Before</p>
          {edit.oldText === undefined ? (
            <p className="context-support-note">Original text unavailable.</p>
          ) : (
            <pre className="context-edit-before">{edit.oldText}</pre>
          )}
          <p className="context-edit-label">
            After{edit.newText === "" ? " (empty replacement)" : ""}
          </p>
          <pre className="context-edit-after">{edit.newText}</pre>
        </article>
      ))}
    </div>
  );
}

export function FeatureResults({
  result,
  method,
  selection,
  onLocation,
  onSuggest,
  onClearAnnotations,
}: {
  result: NavigationResult;
  method: Method;
  selection: Selection;
  onLocation: (location: CodeLocation) => void;
  onSuggest: (action: ReviewAction, selection: Selection) => Promise<void>;
  onClearAnnotations: () => void;
}) {
  const epoch = useRef(0);
  const [adding, setAdding] = useState<number>();
  const [added, setAdded] = useState<Set<number>>(() => new Set());
  const [error, setError] = useState("");
  const [cleared, setCleared] = useState(false);
  useEffect(
    () => () => {
      epoch.current++;
    },
    [],
  );
  async function suggest(action: ReviewAction, index: number) {
    const current = ++epoch.current;
    setAdding(index);
    setError("");
    try {
      await onSuggest(action, selection);
      if (epoch.current === current)
        setAdded((previous) => new Set([...previous, index]));
    } catch (cause) {
      if (epoch.current === current)
        setError(
          cause instanceof Error
            ? cause.message
            : "Could not add these review suggestions.",
        );
    } finally {
      if (epoch.current === current) setAdding(undefined);
    }
  }
  const hasAnnotations =
    method === "documentHighlights" || method === "inlayHints";
  const hasContent = !!(
    result.hover ||
    result.locations?.length ||
    result.items?.length ||
    result.actions?.length ||
    result.edits?.length ||
    hasAnnotations ||
    method === "goTo"
  );
  return (
    <div className="context-feature-results">
      {method === "goTo" ? (
        <>
          <Locations
            label="Definitions"
            locations={result.definitions || []}
            onLocation={onLocation}
          />
          <Locations
            label="References"
            locations={result.references || []}
            onLocation={onLocation}
          />
        </>
      ) : (
        !!result.locations?.length && (
          <Locations
            label="Destinations"
            locations={result.locations}
            onLocation={onLocation}
          />
        )
      )}
      {result.hover && (
        <pre className="context-hover-result">{result.hover}</pre>
      )}
      {!!result.items?.length && (
        <Items items={result.items} onLocation={onLocation} />
      )}
      {hasAnnotations && (
        <>
          <p className="context-support-note" role="status">
            {cleared
              ? "Inline annotations cleared."
              : method === "documentHighlights"
                ? `${result.highlights?.length || 0} occurrence ${result.highlights?.length === 1 ? "range" : "ranges"} highlighted in this revision.`
                : `${result.hints?.length || 0} inlay ${result.hints?.length === 1 ? "hint" : "hints"} shown in this revision.`}
          </p>
          <button
            type="button"
            disabled={cleared}
            onClick={() => {
              onClearAnnotations();
              setCleared(true);
            }}
          >
            Clear inline annotations
          </button>
        </>
      )}
      {!!result.actions?.length &&
        result.actions.map((action, index) => (
          <details className="context-code-action" key={index}>
            <summary>{action.title}</summary>
            {action.disabledReason && (
              <p className="context-support-note">{action.disabledReason}</p>
            )}
            {!!action.edits?.length && (
              <EditPreview edits={action.edits} onLocation={onLocation} />
            )}
            {method === "codeActions" && (
              <button
                type="button"
                disabled={
                  !!action.disabledReason ||
                  !action.edits?.length ||
                  adding !== undefined ||
                  added.has(index)
                }
                onClick={() => void suggest(action, index)}
              >
                {added.has(index)
                  ? "Added to review"
                  : adding === index
                    ? "Adding review suggestions..."
                    : "Add review suggestions"}
              </button>
            )}
          </details>
        ))}
      {!!result.edits?.length && (
        <>
          <p className="context-support-note">
            Read-only rename preview. These changes have not been applied.
          </p>
          <EditPreview edits={result.edits} onLocation={onLocation} />
        </>
      )}
      {error && (
        <p className="context-support-note" role="alert">
          {error}
        </p>
      )}
      {result.message && (
        <p className="context-support-note" role="status">
          {result.message}
        </p>
      )}
      {!hasContent && !result.message && (
        <p className="context-support-note" role="status">
          {method === "diagnostics"
            ? "No diagnostics reported."
            : "No results returned."}
        </p>
      )}
    </div>
  );
}
