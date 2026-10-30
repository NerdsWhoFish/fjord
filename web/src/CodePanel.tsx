import { useEffect, useMemo, useRef, useState } from "react";
import { linePosition, parsePatch, tokenize } from "./diff";
import type {
  ChangedFile,
  Method,
  PullRequest,
  Selection,
  SourceView,
} from "./types";
import { pageRows, sourceWindow } from "./window";

interface Props {
  pr: PullRequest;
  file: ChangedFile;
  selection?: Selection;
  source?: SourceView;
  busy: boolean;
  method: Method;
  onMethod: (method: Method) => void;
  onSelect: (selection: Selection) => void;
  onNavigate: () => void;
  onSource: (side: "LEFT" | "RIGHT") => void;
  onCloseSource: () => void;
  onComment: (selection: Selection) => void;
  onAsk: () => void;
}

function Tokens({
  code,
  onSelect,
}: {
  code: string;
  onSelect: (word: string, column: number) => void;
}) {
  if (code.length > 4096)
    return (
      <span>
        {code.slice(0, 4096)}
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
        {code}
        <span className="long-line-notice">
          {" "}
          [Symbol navigation is disabled for this unusually dense line.]
        </span>
      </span>
    );
  return (
    <>
      {tokens.map((token, i) =>
        token.symbol ? (
          <button
            type="button"
            className="code-token"
            key={i}
            onClick={() => onSelect(token.text, token.column)}
            aria-label={`Inspect ${token.text}`}
          >
            {token.text}
          </button>
        ) : (
          <span key={i}>{token.text}</span>
        ),
      )}
    </>
  );
}

export function CodePanel({
  pr,
  file,
  selection,
  source,
  busy,
  method,
  onMethod,
  onSelect,
  onNavigate,
  onSource,
  onCloseSource,
  onComment,
  onAsk,
}: Props) {
  const sourcePanel = useRef<HTMLDivElement>(null);
  const diffPanel = useRef<HTMLDivElement>(null);
  const [diffPage, setDiffPage] = useState(0);
  const [sourceStart, setSourceStart] = useState(0);
  const sourceLines = useMemo(
    () => source?.content.split("\n") || [],
    [source?.content],
  );
  const lines = useMemo(() => parsePatch(file.patch || ""), [file.patch]);
  const diffWindow = pageRows(lines, diffPage);
  const visibleSource = sourceLines.slice(sourceStart, sourceStart + 200);
  useEffect(() => {
    setDiffPage(0);
  }, [file.path]);
  useEffect(() => {
    setSourceStart(sourceWindow(sourceLines.length, source?.highlight).start);
  }, [source, sourceLines.length]);
  useEffect(() => {
    if (diffPanel.current) diffPanel.current.scrollTop = 0;
  }, [diffPage]);
  useEffect(() => {
    const scroller =
      sourcePanel.current?.querySelector<HTMLElement>(".source-scroll");
    if (scroller)
      scroller.scrollTop =
        source?.highlight !== undefined
          ? Math.max(
              0,
              (source.highlight - sourceStart) * 27 - scroller.clientHeight / 2,
            )
          : 0;
  }, [source, sourceStart]);
  return (
    <section
      className={`code-stage ${source ? "with-source" : ""}`}
      aria-label="Code review"
    >
      <div className="diff-pane">
        <div className="panel-heading">
          <div>
            <span className="eyebrow">CHANGED FILE</span>
            <h2>{file.path}</h2>
          </div>
          <span className="file-status">{file.status}</span>
        </div>
        <div className="code-toolbar">
          <span>
            <b className="addition">+{file.additions}</b>{" "}
            <b className="deletion">-{file.deletions}</b>
          </span>
          <div className="button-group">
            <button
              disabled={busy || file.status === "added"}
              onClick={() => onSource("LEFT")}
            >
              Base source
            </button>
            <button
              disabled={busy || file.status === "removed"}
              onClick={() => onSource("RIGHT")}
            >
              Head source
            </button>
          </div>
        </div>
        {diffWindow.last > 0 && (
          <div className="page-controls">
            <button
              disabled={diffWindow.current === 0}
              onClick={() => setDiffPage(diffWindow.current - 1)}
            >
              Previous diff page
            </button>
            <span>
              Rows {diffWindow.start + 1}-{diffWindow.end} of {lines.length}
            </span>
            <button
              disabled={diffWindow.current === diffWindow.last}
              onClick={() => setDiffPage(diffWindow.current + 1)}
            >
              Next diff page
            </button>
          </div>
        )}
        <div
          className="diff-scroll"
          ref={diffPanel}
          tabIndex={0}
          role="region"
          aria-label={`Diff for ${file.path}`}
        >
          {lines.length ? (
            <div className="code-table">
              {diffWindow.rows.map((line, index) => {
                const position = linePosition(line);
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
                return (
                  <div className={`code-row ${line.kind}`} key={index}>
                    <span
                      className="line-number"
                      title={
                        line.oldLine ? `Base line ${line.oldLine}` : undefined
                      }
                    >
                      {line.oldLine && (
                        <span className="sr-only">Base line </span>
                      )}
                      {line.oldLine}
                    </span>
                    <span
                      className="line-number"
                      title={
                        line.newLine ? `Head line ${line.newLine}` : undefined
                      }
                    >
                      {line.newLine && (
                        <span className="sr-only">Head line </span>
                      )}
                      {line.newLine}
                    </span>
                    <span className="diff-marker" aria-hidden="true">
                      {line.kind === "add"
                        ? "+"
                        : line.kind === "delete"
                          ? "-"
                          : ""}
                    </span>
                    <code>
                      {position ? (
                        <Tokens
                          code={line.text}
                          onSelect={(word, column) =>
                            onSelect(selectionFor(word, column))
                          }
                        />
                      ) : (
                        line.text
                      )}
                    </code>
                    {position && (
                      <button
                        className="add-comment"
                        title={`Comment on ${position.side === "LEFT" ? "base" : "head"} line ${position.line}`}
                        aria-label={`Comment on ${position.side === "LEFT" ? "base" : "head"} line ${position.line}`}
                        onClick={() => onComment(selectionFor("", 0))}
                      >
                        +
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="empty-message">
              <h3>GitHub did not provide a text diff</h3>
              <p>
                This file may be binary, too large, or have no textual changes.
                Open its source when available, or inspect it on GitHub.
              </p>
            </div>
          )}
        </div>
        <div className="code-footnote">
          <span className="live-dot" />
          Select a symbol to follow the code. Use + to leave a line comment.
        </div>
      </div>
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
            <button
              className="icon-button"
              onClick={onCloseSource}
              aria-label="Close source pane"
            >
              x
            </button>
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
                return (
                  <div
                    className={`code-row source-row ${source.highlight === i ? "target-line" : ""}`}
                    key={i}
                  >
                    <span className="line-number">{i + 1}</span>
                    <code>
                      <Tokens
                        code={code}
                        onSelect={(word, character) =>
                          onSelect({
                            path: source.path,
                            line: i + 1,
                            side: source.sha === pr.baseSHA ? "LEFT" : "RIGHT",
                            sha: source.sha,
                            character,
                            word,
                            code,
                          })
                        }
                      />
                    </code>
                  </div>
                );
              })}
            </div>
          </div>
        </div>
      )}
      {selection && (
        <div className="symbol-bar" aria-label="Selected symbol actions">
          <div>
            <span className="eyebrow">FOLLOW THE CODE</span>
            <strong>{selection.word || `Line ${selection.line}`}</strong>
            <span className="symbol-location">
              {selection.side === "LEFT" ? "base" : "head"}:{selection.line}
            </span>
          </div>
          <label className="sr-only" htmlFor="navigation-method">
            Navigation action
          </label>
          <select
            id="navigation-method"
            value={method}
            onChange={(e) => onMethod(e.target.value as Method)}
          >
            <option value="definition">Go to definition</option>
            <option value="implementation">Go to implementation</option>
            <option value="references">Find references</option>
            <option value="hover">Type information</option>
          </select>
          <button
            className="primary"
            disabled={busy || !selection.word}
            onClick={onNavigate}
          >
            {busy ? "Following..." : "Follow symbol"}
          </button>
          <button onClick={onAsk}>Ask AI about selection</button>
        </div>
      )}
    </section>
  );
}
