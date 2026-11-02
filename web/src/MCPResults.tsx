import { FeatureResults } from "./FeatureResults";
import { methodLabels } from "./feature-methods";
import type {
  CodeLocation,
  Method,
  NavigationResult,
  ReviewAction,
  Selection,
} from "./types";
import "./mcp-results.css";

export function MCPResults({
  method,
  result,
  selection,
  onLocation,
  onSuggest,
  onClearAnnotations,
  onClose,
}: {
  method: Method;
  result: NavigationResult;
  selection: Selection;
  onLocation: (location: CodeLocation, selected: Selection) => void;
  onSuggest: (action: ReviewAction, selected: Selection) => Promise<void>;
  onClearAnnotations: () => void;
  onClose: () => void;
}) {
  return (
    <section className="mcp-results" aria-label="Assistant code results">
      <div className="section-title">
        <span className="eyebrow">ASSISTANT CODE CONTEXT</span>
        <button
          className="icon-button"
          onClick={onClose}
          aria-label="Return to file outline"
        >
          x
        </button>
      </div>
      <h2>{methodLabels[method]}</h2>
      <p className="outline-path">
        {selection.path}:{selection.line}
      </p>
      <p className="outline-revision">
        {selection.side === "LEFT" ? "Base" : "Head"} /{" "}
        {selection.sha.slice(0, 7)}
      </p>
      <FeatureResults
        result={result}
        method={method}
        selection={selection}
        onLocation={(location) => onLocation(location, selection)}
        onSuggest={onSuggest}
        onClearAnnotations={onClearAnnotations}
      />
    </section>
  );
}
