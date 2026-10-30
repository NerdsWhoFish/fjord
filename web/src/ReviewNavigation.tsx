import type { CodeLocation, Method, NavigationResult } from "./types";

export function ReviewNavigation({
  result,
  method,
  busy,
  onLocation,
  onDismiss,
}: {
  result?: NavigationResult;
  method: Method;
  busy: boolean;
  onLocation: (location: CodeLocation) => void;
  onDismiss: () => void;
}) {
  if (!result && !busy) return null;
  const locations = result?.locations || [];
  return (
    <div className="review-navigation" aria-label="Code navigation results">
      <div className="review-navigation-controls">
        <span role="status">
          {busy
            ? "Resolving code navigation..."
            : method === "hover"
              ? "Type information"
              : `${locations.length} ${method === "references" ? "references" : "destinations"}`}
        </span>
        {!!locations.length && (
          <select
            aria-label="Navigation destinations"
            value=""
            disabled={busy}
            onChange={(event) => {
              const location = locations[Number(event.target.value)];
              if (location) onLocation(location);
            }}
          >
            <option value="" disabled>
              Choose a destination
            </option>
            {locations.map((location, index) => (
              <option key={index} value={index}>
                {location.path}:{location.line + 1}
              </option>
            ))}
          </select>
        )}
        {result && (
          <button type="button" onClick={onDismiss}>
            Dismiss
          </button>
        )}
      </div>
      {result?.hover && <pre>{result.hover}</pre>}
      {!busy && result && !result.hover && !locations.length && (
        <p role="status">
          {result.message || "The language server returned no destinations."}
        </p>
      )}
    </div>
  );
}
