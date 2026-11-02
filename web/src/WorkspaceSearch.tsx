import { useEffect, useRef, useState } from "react";
import type {
  CodeLocation,
  NavigationResult,
  ReviewItem,
  Selection,
} from "./types";
import type { InspectCode } from "./OutlinePanel";

export function searchableSymbols(items: ReviewItem[]): ReviewItem[] {
  return items.flatMap((item) => [
    ...(item.location ? [item] : []),
    ...searchableSymbols(item.children || []),
  ]);
}

export function WorkspaceSearch({
  selected,
  inspect,
  onLocation,
}: {
  selected?: Selection;
  inspect: InspectCode;
  onLocation: (location: CodeLocation, selected: Selection) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const request = useRef(0);
  const [query, setQuery] = useState("");
  const [result, setResult] = useState<NavigationResult>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const key = selected ? `${selected.sha}:${selected.path}` : "";
  useEffect(() => {
    request.current++;
    setResult(undefined);
    setError("");
    setBusy(false);
    return () => {
      request.current++;
    };
  }, [key]);
  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if (
        (event.metaKey || event.ctrlKey) &&
        event.key.toLowerCase() === "k" &&
        !event.altKey
      ) {
        if (document.querySelector("dialog[open]")) return;
        event.preventDefault();
        dialog.current?.showModal();
      }
    };
    window.addEventListener("keydown", shortcut);
    return () => window.removeEventListener("keydown", shortcut);
  }, []);
  async function search() {
    if (!selected || !query.trim()) return;
    const current = ++request.current;
    setBusy(true);
    setError("");
    setResult(undefined);
    try {
      const next = await inspect(selected, "workspaceSymbols", {
        query: query.trim(),
      });
      if (current === request.current) setResult(next);
    } catch (error) {
      if (current === request.current)
        setError(
          error instanceof Error
            ? error.message
            : "Could not search workspace symbols.",
        );
    } finally {
      if (current === request.current) setBusy(false);
    }
  }
  const symbols = searchableSymbols(result?.items || []);
  return (
    <>
      <button
        className="workspace-search-trigger"
        onClick={() => dialog.current?.showModal()}
        aria-keyshortcuts="Meta+K Control+K"
      >
        Search symbols <kbd>Ctrl / Cmd K</kbd>
      </button>
      <dialog
        ref={dialog}
        className="workspace-search-dialog"
        aria-labelledby="workspace-search-title"
        onClose={() => {
          request.current++;
          setBusy(false);
        }}
      >
        <div className="dialog-heading">
          <h2 id="workspace-search-title">Workspace symbols</h2>
          <button
            className="icon-button"
            onClick={() => dialog.current?.close()}
            aria-label="Close symbol search"
          >
            x
          </button>
        </div>
        <p className="muted small">
          Search the {selected?.side === "LEFT" ? "base" : "head"} revision
          using the active file's language server, including unchanged files.
        </p>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void search();
          }}
          className="workspace-search-form"
        >
          <label className="sr-only" htmlFor="workspace-symbol-query">
            Search workspace symbols
          </label>
          <input
            id="workspace-symbol-query"
            autoFocus
            value={query}
            maxLength={256}
            onChange={(event) => {
              setQuery(event.target.value);
              request.current++;
              setBusy(false);
              setResult(undefined);
              setError("");
            }}
            placeholder="Function, type, or variable..."
            autoComplete="off"
            spellCheck={false}
          />
          <button
            className="primary"
            disabled={busy || !selected || !query.trim()}
          >
            Search
          </button>
        </form>
        {busy && <p role="status">Searching symbols...</p>}
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <ul className="workspace-symbol-results">
          {symbols.map((item, index) => (
            <li key={index}>
              <button
                onClick={() => {
                  if (!selected || !item.location) return;
                  dialog.current?.close();
                  onLocation(item.location, selected);
                }}
              >
                <span>
                  <strong>{item.label}</strong>
                  <span className="outline-symbol-kind">{item.kind}</span>
                </span>
                <code>
                  {item.location!.path}:{item.location!.line + 1}
                </code>
              </button>
            </li>
          ))}
        </ul>
        {result?.message && (
          <p role="status" className="muted small">
            {result.message}
          </p>
        )}
        {result && !symbols.length && !result.message && (
          <p role="status">No matching symbols.</p>
        )}
        {!selected && (
          <p role="status">Choose a source file to search its workspace.</p>
        )}
      </dialog>
    </>
  );
}
