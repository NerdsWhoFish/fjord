import { useEffect, useId, useMemo, useRef, useState } from "react";
import type { CSSProperties } from "react";
import type { ChangedFile } from "./types";
import {
  buildFileTree,
  fileKind,
  fileStatus,
  filterChangedFiles,
  folderPaths,
  statusLabels,
} from "./file-tree";
import type { FileStatus, FileTreeNode, StatusFilter } from "./file-tree";
import "./file-tree.css";

interface FileTreeProps {
  files: ChangedFile[];
  selectedFile: string;
  viewed: ReadonlySet<string>;
  filter: string;
  onFilter: (value: string) => void;
  onSelect: (path: string) => void;
}

const noCollapsedFolders: ReadonlySet<string> = new Set();

function DocumentIcon({ status }: { status: FileStatus }) {
  return (
    <svg
      className={`tree-document tree-status-${status}`}
      viewBox="0 0 16 16"
      aria-hidden="true"
    >
      <path d="M9.5 1.5H3.5v13h9v-10zM9.5 1.5v3h3" />
      {status === "added" ? (
        <path d="M5.5 9h5M8 6.5v5" />
      ) : status === "removed" ? (
        <path d="M5.5 9h5" />
      ) : status === "renamed" || status === "copied" ? (
        <path d="M5 9h5m-2-2 2 2-2 2" />
      ) : (
        <path d="m5.5 10 4-4 1.5 1.5-4 4h-1.5z" />
      )}
    </svg>
  );
}

function FolderIcon({ expanded }: { expanded: boolean }) {
  return (
    <svg className="tree-folder-icon" viewBox="0 0 16 16" aria-hidden="true">
      {expanded ? (
        <path d="M1.5 4.5V3h4l1.5 1.5h6v2H3L1.5 13h11l2-6.5H3" />
      ) : (
        <path d="M1.5 13V3h4l1.5 1.5h6.5V13z" />
      )}
    </svg>
  );
}

export function FileTree({
  files,
  selectedFile,
  viewed,
  filter,
  onFilter,
  onSelect,
}: FileTreeProps) {
  const id = useId();
  const [status, setStatus] = useState<StatusFilter>("all");
  const [unviewed, setUnviewed] = useState(false);
  const [optionsOpen, setOptionsOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());
  const [filteredCollapsed, setFilteredCollapsed] = useState({
    key: "",
    paths: noCollapsedFolders,
  });
  const options = useRef<HTMLDivElement>(null);
  const optionsButton = useRef<HTMLButtonElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const filtering = !!filter.trim() || status !== "all" || unviewed;
  const filterKey = JSON.stringify([filter, status, unviewed]);
  const hiddenFolders = filtering
    ? filteredCollapsed.key === filterKey
      ? filteredCollapsed.paths
      : noCollapsedFolders
    : collapsed;
  const matches = useMemo(
    () => filterChangedFiles(files, filter, status, unviewed, viewed),
    [files, filter, status, unviewed, viewed],
  );
  const nodes = useMemo(() => buildFileTree(matches), [matches]);
  const folders = useMemo(() => folderPaths(nodes), [nodes]);
  const availableStatuses = [
    ...new Set(files.map((file) => fileStatus(file.status))),
  ];
  const allCollapsed =
    folders.length > 0 && folders.every((path) => hiddenFolders.has(path));

  useEffect(() => {
    if (!optionsOpen) return;
    function dismiss(event: PointerEvent) {
      if (
        event.target instanceof Node &&
        !options.current?.contains(event.target)
      )
        setOptionsOpen(false);
    }
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, [optionsOpen]);

  function updateCollapsed(next: ReadonlySet<string>) {
    if (filtering) setFilteredCollapsed({ key: filterKey, paths: next });
    else setCollapsed(next);
  }
  function toggleFolder(path: string) {
    const next = new Set(hiddenFolders);
    if (next.has(path)) next.delete(path);
    else next.add(path);
    updateCollapsed(next);
  }
  function resetFilters() {
    onFilter("");
    setStatus("all");
    setUnviewed(false);
    input.current?.focus();
  }

  function renderNodes(children: FileTreeNode[], depth = 0) {
    return children.map((node) => {
      const indent = { "--tree-depth": Math.min(depth, 8) } as CSSProperties;
      if (node.kind === "folder") {
        const expanded = !hiddenFolders.has(node.path);
        const childrenID = `${id}-folder-${encodeURIComponent(node.path)}`;
        return (
          <li key={`folder:${node.path}`} className="tree-folder">
            <button
              className="tree-row tree-folder-row"
              style={indent}
              type="button"
              aria-expanded={expanded}
              aria-controls={childrenID}
              title={node.path}
              onClick={() => toggleFolder(node.path)}
            >
              <svg
                className={`tree-chevron ${expanded ? "expanded" : ""}`}
                viewBox="0 0 12 12"
                aria-hidden="true"
              >
                <path d="m4.5 2.5 3.5 3.5-3.5 3.5" />
              </svg>
              <FolderIcon expanded={expanded} />
              <span className="tree-name">{node.name}</span>
            </button>
            <ul id={childrenID} hidden={!expanded}>
              {renderNodes(node.children, depth + 1)}
            </ul>
          </li>
        );
      }
      const file = node.file;
      const state = fileStatus(file.status);
      const wasViewed = viewed.has(file.path);
      const renamed = file.previousPath && file.previousPath !== file.path;
      const description = `${file.path}, ${statusLabels[state].toLowerCase()}, ${fileKind(file.path)}${renamed ? `, previously ${file.previousPath}` : ""}${wasViewed ? ", viewed" : ""}`;
      return (
        <li key={`file:${node.path}`}>
          <button
            type="button"
            className={`tree-row tree-file-row ${selectedFile === file.path ? "selected" : ""}`}
            style={indent}
            onClick={() => onSelect(file.path)}
            aria-current={selectedFile === file.path ? "true" : undefined}
            aria-label={description}
            title={
              renamed
                ? `${file.path}\nPreviously: ${file.previousPath}`
                : file.path
            }
          >
            <span className="tree-chevron-spacer" aria-hidden="true" />
            <DocumentIcon status={state} />
            <span className="tree-name">{node.name}</span>
            {renamed && (
              <span className="tree-renamed-mark" aria-hidden="true">
                &larr;
              </span>
            )}
            {wasViewed && (
              <svg
                className="tree-viewed"
                viewBox="0 0 16 16"
                aria-hidden="true"
              >
                <path d="m3 8 3 3 7-7" />
              </svg>
            )}
          </button>
        </li>
      );
    });
  }

  return (
    <div className="file-tree">
      <div className="tree-toolbar">
        <div className="tree-search">
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <circle cx="6.5" cy="6.5" r="4" />
            <path d="m9.5 9.5 4 4" />
          </svg>
          <label className="sr-only" htmlFor={`${id}-search`}>
            Filter changed files
          </label>
          <input
            id={`${id}-search`}
            ref={input}
            type="search"
            placeholder="Filter files..."
            value={filter}
            onChange={(event) => onFilter(event.target.value)}
            autoComplete="off"
            spellCheck={false}
          />
        </div>
        <div
          className="tree-options"
          ref={options}
          onBlur={(event) => {
            if (
              event.relatedTarget instanceof Node &&
              !event.currentTarget.contains(event.relatedTarget)
            )
              setOptionsOpen(false);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape" && optionsOpen) {
              event.preventDefault();
              setOptionsOpen(false);
              optionsButton.current?.focus();
            }
          }}
        >
          <button
            className={`tree-options-trigger ${status !== "all" || unviewed ? "filtered" : ""}`}
            type="button"
            ref={optionsButton}
            aria-label="File filter options"
            aria-expanded={optionsOpen}
            aria-controls={`${id}-options`}
            title="Filter by status or viewed state"
            onClick={() => setOptionsOpen((open) => !open)}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M2 4h12M4 8h8M6 12h4" />
            </svg>
          </button>
          {optionsOpen && (
            <div className="tree-options-panel" id={`${id}-options`}>
              <label htmlFor={`${id}-status`}>Change status</label>
              <select
                id={`${id}-status`}
                value={status}
                onChange={(event) =>
                  setStatus(event.target.value as StatusFilter)
                }
              >
                <option value="all">All changes</option>
                {(Object.keys(statusLabels) as FileStatus[])
                  .filter((state) => availableStatuses.includes(state))
                  .map((state) => (
                    <option value={state} key={state}>
                      {statusLabels[state]}
                    </option>
                  ))}
              </select>
              <label className="tree-unviewed">
                <input
                  type="checkbox"
                  checked={unviewed}
                  onChange={(event) => setUnviewed(event.target.checked)}
                />
                Only unviewed files
              </label>
              <button
                className="tree-option-action"
                type="button"
                disabled={!folders.length}
                onClick={() => {
                  updateCollapsed(allCollapsed ? new Set() : new Set(folders));
                  setOptionsOpen(false);
                  optionsButton.current?.focus();
                }}
              >
                {allCollapsed ? "Expand all folders" : "Collapse all folders"}
              </button>
              {filtering && (
                <button
                  className="tree-option-action"
                  type="button"
                  onClick={() => {
                    resetFilters();
                    setOptionsOpen(false);
                  }}
                >
                  Clear filters
                </button>
              )}
            </div>
          )}
        </div>
      </div>
      {filtering && (
        <p className="tree-filter-summary" role="status">
          {matches.length} of {files.length} files
        </p>
      )}
      <nav className="tree-scroll" aria-label="Changed files">
        {nodes.length ? (
          <ul className="tree-root">{renderNodes(nodes)}</ul>
        ) : (
          <div className="tree-empty">
            <p>{files.length ? "No matching files." : "No changed files."}</p>
            {filtering && (
              <button
                type="button"
                className="tree-clear"
                onClick={resetFilters}
              >
                Clear filters
              </button>
            )}
          </div>
        )}
      </nav>
    </div>
  );
}
