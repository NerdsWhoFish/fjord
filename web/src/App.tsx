import { useRef, useState } from "react";
import { api } from "./api";
import { Assistant } from "./Assistant";
import { CodePanel } from "./CodePanel";
import { FileTree } from "./FileTree";
import { SettingsMenu } from "./SettingsMenu";
import { demoBaseSource, demoPR, demoSource, demoThreads } from "./demo";
import { emptyDraft, readDraft, ReviewPanel, writeDraft } from "./ReviewPanel";
import type { ReviewDraft } from "./ReviewPanel";
import { captureError, startTelemetry } from "./telemetry";
import type {
  CodeLocation,
  Config,
  Method,
  NavigationResult,
  PullRequest,
  Selection,
  SourceView,
  ThreadComment,
} from "./types";

function Brand() {
  return (
    <a href="/" className="brand" aria-label="Fjord home">
      <span className="brand-mark" aria-hidden="true">
        <i />
        <i />
        <i />
      </span>
      <span>
        fjord<span className="brand-dot">.</span>
      </span>
    </a>
  );
}

export function App() {
  const [url, setURL] = useState("");
  const [pr, setPR] = useState<PullRequest>();
  const [demo, setDemo] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [selectedFile, setSelectedFile] = useState("");
  const [selection, setSelection] = useState<Selection>();
  const [source, setSource] = useState<SourceView>();
  const [method, setMethod] = useState<Method>("definition");
  const [navigating, setNavigating] = useState(false);
  const [navResult, setNavResult] = useState<NavigationResult>();
  const [navWorkspace, setNavWorkspace] = useState("");
  const [navSHA, setNavSHA] = useState("");
  const [draft, setDraft] = useState<ReviewDraft>(emptyDraft);
  const [pendingComment, setPendingComment] = useState<Selection>();
  const [threads, setThreads] = useState<ThreadComment[]>([]);
  const [threadsError, setThreadsError] = useState("");
  const [config, setConfig] = useState<Config>(
    globalThis.__FJORD_CONFIG__ || {
      aiEnabled: false,
      telemetryEnabled: false,
    },
  );
  const [tab, setTab] = useState<"review" | "assistant">("review");
  const [mobileTab, setMobileTab] = useState<"code" | "review">("code");
  const [viewed, setViewed] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState("");
  const epoch = useRef(0);
  const prEpoch = useRef(0);
  const threadEpoch = useRef(0);
  const workspaces = useRef(new Map<string, string>());
  const file = pr?.files.find((f) => f.path === selectedFile);

  function changeDraft(next: ReviewDraft) {
    setDraft(next);
    if (pr && !writeDraft(pr, demo, next))
      setNotice(
        "Browser storage is unavailable. Keep this tab open to preserve your draft.",
      );
  }
  function chooseFile(path: string) {
    epoch.current++;
    setSelectedFile(path);
    setSource(undefined);
    setSelection(undefined);
    setNavResult(undefined);
    setNavigating(false);
    setMobileTab("code");
  }
  function selectSymbol(next: Selection) {
    epoch.current++;
    setNavigating(false);
    setSelection(next);
    setNavResult(undefined);
  }
  function acceptPR(next: PullRequest, isDemo: boolean) {
    epoch.current++;
    prEpoch.current++;
    threadEpoch.current++;
    setPR(next);
    setDemo(isDemo);
    setSelectedFile(next.files[0]?.path || "");
    setSource(undefined);
    setSelection(undefined);
    setNavResult(undefined);
    setDraft(readDraft(next, isDemo));
    setViewed(new Set());
    setThreads([]);
    setThreadsError("");
    setNotice("");
    setError("");
    setFilter("");
    workspaces.current.clear();
    setMobileTab("code");
  }
  async function reloadThreads(next = pr, isDemo = demo) {
    if (!next) return;
    if (isDemo) {
      setThreads(demoThreads);
      return;
    }
    const currentPR = prEpoch.current;
    const currentRequest = ++threadEpoch.current;
    setThreadsError("");
    try {
      const result = await api<{ comments: ThreadComment[] }>("comments", {
        owner: next.owner,
        repo: next.repo,
        number: next.number,
      });
      if (
        prEpoch.current === currentPR &&
        threadEpoch.current === currentRequest
      )
        setThreads(result.comments || []);
    } catch (error) {
      if (
        prEpoch.current === currentPR &&
        threadEpoch.current === currentRequest
      )
        setThreadsError(
          error instanceof Error
            ? error.message
            : "Could not load the conversation.",
        );
    }
  }
  async function openPR(event: React.FormEvent) {
    event.preventDefault();
    setError("");
    setLoading(true);
    try {
      const next = await api<PullRequest>("pr", { url: url.trim() });
      acceptPR(next, false);
      void reloadThreads(next, false);
      const currentPR = prEpoch.current;
      api<Config>("config")
        .then((nextConfig) => {
          if (prEpoch.current !== currentPR) return;
          setConfig(nextConfig);
          if (nextConfig.telemetryEnabled) startTelemetry();
        })
        .catch(() => {
          if (prEpoch.current === currentPR)
            setNotice(
              "Optional AI and telemetry configuration could not be loaded. Review and navigation remain available.",
            );
        });
    } catch (error) {
      captureError(error, "pr.load");
      setError(
        error instanceof Error
          ? error.message
          : "Could not open this pull request.",
      );
    } finally {
      setLoading(false);
    }
  }
  function openDemo() {
    acceptPR(demoPR, true);
    setThreads(demoThreads);
    setConfig({ aiEnabled: false, telemetryEnabled: false });
  }
  async function openSource(side: "LEFT" | "RIGHT") {
    if (!pr || !file) return;
    const requestEpoch = ++epoch.current;
    setNavigating(true);
    setError("");
    const path = side === "LEFT" ? file.previousPath || file.path : file.path;
    const sha = side === "LEFT" ? pr.baseSHA : pr.headSHA;
    try {
      const result = demo
        ? {
            content:
              (side === "LEFT" ? demoBaseSource : demoSource)[path] || "",
          }
        : await api<{ content: string }>("content", {
            owner: pr.owner,
            repo: pr.repo,
            sha,
            path,
          });
      if (epoch.current === requestEpoch)
        setSource({ path, sha, content: result.content });
    } catch (error) {
      if (epoch.current === requestEpoch)
        setError(
          error instanceof Error ? error.message : "Source is unavailable.",
        );
    } finally {
      if (epoch.current === requestEpoch) setNavigating(false);
    }
  }
  async function showLocation(
    location: CodeLocation,
    workspace = navWorkspace,
    sha = navSHA,
    requestEpoch = ++epoch.current,
  ) {
    setNavigating(true);
    try {
      const result = demo
        ? {
            content:
              (sha === pr?.baseSHA ? demoBaseSource : demoSource)[
                location.path
              ] || "",
          }
        : await api<{ content: string }>("workspace/file", {
            id: workspace,
            path: location.path,
          });
      if (epoch.current === requestEpoch)
        setSource({
          path: location.path,
          sha,
          content: result.content,
          highlight: location.line,
          workspace,
        });
    } finally {
      if (epoch.current === requestEpoch) setNavigating(false);
    }
  }
  async function navigate(nextSelection = selection, nextMethod = method) {
    if (!pr || !nextSelection) return;
    setSelection(nextSelection);
    setMethod(nextMethod);
    const requestEpoch = ++epoch.current;
    setNavigating(true);
    setError("");
    setNavResult(undefined);
    try {
      let workspace = workspaces.current.get(nextSelection.sha);
      if (!workspace) {
        workspace = demo
          ? "demo"
          : (
              await api<{ id: string }>("workspace", {
                owner: pr.owner,
                repo: pr.repo,
                sha: nextSelection.sha,
              })
            ).id;
        workspaces.current.set(nextSelection.sha, workspace);
      }
      const demoSymbol: Record<string, [string, number]> = {
        backoff: ["internal/harbor/backoff.go", 5],
        Connect: ["internal/harbor/connect.go", 8],
        Connector: ["internal/harbor/connect.go", 4],
        Open: ["internal/harbor/connect.go", 5],
      };
      const target = demoSymbol[nextSelection.word];
      const result: NavigationResult = demo
        ? {
            locations:
              nextMethod === "hover" || !target
                ? []
                : [
                    {
                      path: target[0],
                      line: target[1],
                      character: 0,
                      endLine: target[1],
                      endCharacter: nextSelection.word.length,
                    },
                  ],
            hover:
              nextMethod === "hover"
                ? `Illustrative type information for ${nextSelection.word}. Open a real PR for language-server results.`
                : "",
          }
        : await api<NavigationResult>("navigate", {
            id: workspace,
            path: nextSelection.path,
            method: nextMethod,
            line: nextSelection.line - 1,
            character: nextSelection.character,
          });
      if (epoch.current !== requestEpoch) return;
      setNavResult(result);
      setNavWorkspace(workspace);
      setNavSHA(nextSelection.sha);
      if (result.locations?.length === 1)
        await showLocation(
          result.locations[0],
          workspace,
          nextSelection.sha,
          requestEpoch,
        );
    } catch (error) {
      captureError(error, "navigate");
      if (epoch.current === requestEpoch)
        setError(
          error instanceof Error ? error.message : "Navigation is unavailable.",
        );
    } finally {
      if (epoch.current === requestEpoch) setNavigating(false);
    }
  }
  async function submitReview() {
    if (!pr || demo || pr.state !== "open")
      throw new Error("Only open GitHub pull requests can receive reviews.");
    const currentPR = prEpoch.current;
    try {
      const result = await api<{ url: string }>("review", {
        owner: pr.owner,
        repo: pr.repo,
        number: pr.number,
        baseSHA: pr.baseSHA,
        headSHA: pr.headSHA,
        event: draft.event,
        body: draft.body,
        comments: draft.comments.map(
          ({ path, line, side, body, startLine, startSide }) => ({
            path,
            line,
            side,
            body,
            start_line: startLine,
            start_side: startSide,
          }),
        ),
      });
      writeDraft(pr, false, emptyDraft());
      if (prEpoch.current === currentPR) {
        setDraft(emptyDraft());
        setNotice(
          `Review submitted to GitHub.${result.url ? " Open the PR to see your review." : ""}`,
        );
        void reloadThreads();
      }
    } catch (error) {
      captureError(error, "review.submit");
      throw error;
    }
  }
  async function reply(id: number, body: string) {
    if (!pr || demo) throw new Error("Demo replies cannot be sent.");
    const currentPR = prEpoch.current;
    try {
      await api("reply", {
        owner: pr.owner,
        repo: pr.repo,
        number: pr.number,
        commentID: id,
        body,
      });
      if (prEpoch.current === currentPR) await reloadThreads();
    } catch (error) {
      captureError(error, "reply");
      throw error;
    }
  }
  async function ask(question: string) {
    if (!selection) throw new Error("Select some code first.");
    if (demo)
      return "This is a sample response, not an AI analysis. The select statement listens for context cancellation while waiting for the backoff timer. Follow the definition of backoff to check the maximum delay, then verify that Connector.Open also respects the context.";
    try {
      return (
        await api<{ answer: string }>("assist", {
          question,
          code: selection.code,
          path: selection.path,
          patch: "",
        })
      ).answer;
    } catch (error) {
      captureError(error, "assist");
      throw error;
    }
  }

  const openForm = (
    <form className="open-form" onSubmit={openPR}>
      <label className="sr-only" htmlFor="pr-url">
        GitHub pull request URL
      </label>
      <input
        id="pr-url"
        type="url"
        required
        placeholder="https://github.com/owner/repo/pull/123"
        value={url}
        onChange={(e) => setURL(e.target.value)}
        disabled={loading}
      />
      <button className="primary" disabled={loading}>
        {loading ? "Opening..." : "Open pull request"}
      </button>
    </form>
  );
  if (!pr)
    return (
      <div className="welcome">
        <header>
          <Brand />
          <SettingsMenu />
        </header>
        <main className="welcome-main">
          <div className="welcome-copy">
            <span className="eyebrow">GITHUB PR REVIEW, WITH CONTEXT</span>
            <h1>
              Follow the code.
              <br />
              <span>Find your footing.</span>
            </h1>
            <p>
              A diff tells you what changed. Fjord helps you understand what it
              means. Explore definitions, follow implementations, and bring your
              review back to GitHub.
            </p>
            {openForm}
            {error && (
              <p className="error" role="alert">
                {error}
              </p>
            )}
            <button className="demo-link" onClick={openDemo}>
              Explore a sample review <span aria-hidden="true">&rarr;</span>
            </button>
            <p className="connection-note">
              <span className="live-dot" />
              Uses your local GitHub CLI session.
            </p>
          </div>
          <div className="welcome-landscape" aria-hidden="true">
            <div className="contour c1" />
            <div className="contour c2" />
            <div className="contour c3" />
            <div className="channel" />
            <div className="landscape-label">
              <span>BEYOND THE DIFF</span>
              <strong>
                More context.
                <br />
                Better judgment.
              </strong>
              <span>
                01 / EXPLORE &nbsp; 02 / UNDERSTAND &nbsp; 03 / REVIEW
              </span>
            </div>
          </div>
        </main>
        <footer>
          Built for people who read the code.
          <span>GitHub stays the source of truth.</span>
        </footer>
      </div>
    );

  return (
    <div className="workspace">
      <nav className="skip-links" aria-label="Skip navigation">
        <a
          href="#code-content"
          onClick={(event) => {
            event.preventDefault();
            setMobileTab("code");
            requestAnimationFrame(() =>
              document.getElementById("code-content")?.focus(),
            );
          }}
        >
          Skip to code
        </a>
        <a
          href="#review-context"
          onClick={(event) => {
            event.preventDefault();
            setMobileTab("review");
            requestAnimationFrame(() =>
              document.getElementById("review-context")?.focus(),
            );
          }}
        >
          Skip to review and assistance
        </a>
      </nav>
      <aside className="file-rail">
        <Brand />
        <div className="rail-heading">
          <span className="eyebrow">PULL REQUEST</span>
          <span className="pr-number">#{pr.number}</span>
        </div>
        <strong className="repo-name">
          {pr.owner}
          <br />
          <span>{pr.repo}</span>
        </strong>
        <div className="rail-rule" />
        <div className="rail-heading">
          <span>Changed files</span>
          <span className="count">{pr.files.length}</span>
        </div>
        <FileTree
          key={`${pr.owner}/${pr.repo}/${pr.number}/${pr.headSHA}`}
          files={pr.files}
          selectedFile={selectedFile}
          viewed={viewed}
          filter={filter}
          onFilter={setFilter}
          onSelect={chooseFile}
        />
        <div className="rail-bottom">
          <div className="progress-track">
            <span
              style={{
                width: `${pr.files.length ? (viewed.size / pr.files.length) * 100 : 0}%`,
              }}
            />
          </div>
          <p>
            {viewed.size} of {pr.files.length} files viewed
          </p>
          <span className="rail-caption">Every change has a story.</span>
        </div>
      </aside>
      <main className="review-workspace">
        <header className="workspace-header">
          <div>
            <div className="breadcrumb">
              {pr.owner} / {pr.repo}
              <span className={`state-badge ${demo ? "demo" : ""}`}>
                {demo ? "SAMPLE REVIEW" : pr.state}
              </span>
            </div>
            <h1>{pr.title}</h1>
            <p className="branch-line">
              <strong>{pr.author}</strong> wants to merge{" "}
              <code>{pr.headRef}</code>
              <span aria-hidden="true">&rarr;</span>
              <code>{pr.baseRef}</code>
            </p>
          </div>
          <div className="header-actions">
            <SettingsMenu />
            {pr.url && (
              <a
                className="button"
                href={pr.url}
                target="_blank"
                rel="noreferrer"
              >
                Open on GitHub
              </a>
            )}
            <button
              onClick={() => {
                epoch.current++;
                setPR(undefined);
                setNavigating(false);
                setURL("");
              }}
            >
              Switch PR
            </button>
          </div>
        </header>
        {demo && (
          <div className="demo-banner">
            <strong>Sample workspace</strong>
            <span>
              Navigation and AI use illustrative fixtures. GitHub actions are
              disabled.
            </span>
          </div>
        )}
        {error && (
          <div className="error workspace-message" role="alert">
            {error}
            <button onClick={() => setError("")} aria-label="Dismiss error">
              x
            </button>
          </div>
        )}
        {notice && (
          <div className="notice workspace-message" role="status">
            {notice}
            <button onClick={() => setNotice("")} aria-label="Dismiss notice">
              x
            </button>
          </div>
        )}
        <div className="workspace-controls">
          <div className="mobile-files">
            <label className="sr-only" htmlFor="mobile-file">
              Changed file
            </label>
            <select
              id="mobile-file"
              value={selectedFile}
              onChange={(e) => chooseFile(e.target.value)}
            >
              {pr.files.map((f) => (
                <option key={f.path} value={f.path}>
                  {f.path}
                </option>
              ))}
            </select>
          </div>
          <span className="review-caption">THE CHANGE, IN CONTEXT</span>
          {file && (
            <label className="viewed-control">
              <input
                type="checkbox"
                checked={viewed.has(file.path)}
                onChange={(e) =>
                  setViewed((current) => {
                    const next = new Set(current);
                    e.target.checked
                      ? next.add(file.path)
                      : next.delete(file.path);
                    return next;
                  })
                }
              />
              Viewed
            </label>
          )}
          <div className="mobile-tabs">
            <button
              className={mobileTab === "code" ? "active" : ""}
              onClick={() => setMobileTab("code")}
            >
              Code
            </button>
            <button
              className={mobileTab === "review" ? "active" : ""}
              onClick={() => setMobileTab("review")}
            >
              Review ({draft.comments.length})
            </button>
          </div>
        </div>
        <div className={`workspace-body mobile-${mobileTab}`}>
          <div className="code-column" id="code-content" tabIndex={-1}>
            {file ? (
              <CodePanel
                pr={pr}
                file={file}
                selection={selection}
                source={source}
                busy={navigating}
                method={method}
                onMethod={setMethod}
                onSelect={selectSymbol}
                onNavigate={navigate}
                onSource={openSource}
                onCloseSource={() => setSource(undefined)}
                onComment={(next) => {
                  setTab("review");
                  setMobileTab("review");
                  setPendingComment(next);
                }}
                onAsk={() => {
                  setTab("assistant");
                  setMobileTab("review");
                }}
              />
            ) : (
              <div className="empty-message">
                <h2>No changed files</h2>
                <p>This pull request has no files to review.</p>
              </div>
            )}
            {navResult && (
              <section className="navigation-results" aria-live="polite">
                <div className="section-title">
                  <span className="eyebrow">
                    {method === "hover"
                      ? "TYPE INFORMATION"
                      : `${navResult.locations?.length || 0} ${method === "references" ? "REFERENCES" : "RESULTS"}`}
                  </span>
                  <button
                    className="text-button"
                    onClick={() => setNavResult(undefined)}
                  >
                    Dismiss
                  </button>
                </div>
                {navResult.hover && <pre>{navResult.hover}</pre>}
                {!navResult.hover && !navResult.locations?.length && (
                  <p>
                    {navResult.message ||
                      "No results from the language server. The symbol may be external, unresolved, or unsupported."}
                  </p>
                )}
                {navResult.locations?.map((location, i) => (
                  <button
                    className="location-result"
                    disabled={navigating}
                    key={`${location.path}:${location.line}:${i}`}
                    onClick={() => {
                      void showLocation(location).catch((error) =>
                        setError(
                          error instanceof Error
                            ? error.message
                            : "Could not load navigation target.",
                        ),
                      );
                    }}
                  >
                    <code>{location.path}</code>
                    <span>Line {location.line + 1}</span>
                  </button>
                ))}
              </section>
            )}
          </div>
          <aside
            className="context-panel"
            id="review-context"
            tabIndex={-1}
            aria-label="Review and assistance"
          >
            <div className="context-tabs">
              <button
                className={tab === "review" ? "active" : ""}
                onClick={() => setTab("review")}
              >
                Review <span>{draft.comments.length}</span>
              </button>
              <button
                className={tab === "assistant" ? "active" : ""}
                onClick={() => setTab("assistant")}
              >
                AI assistance
              </button>
            </div>
            <div className={tab === "review" ? "" : "hidden-panel"}>
              <ReviewPanel
                key={`${pr.headSHA}:${demo}`}
                pr={pr}
                demo={demo}
                draft={draft}
                onDraft={changeDraft}
                threads={threads}
                threadsError={threadsError}
                onReloadThreads={() => {
                  void reloadThreads();
                }}
                onSubmit={submitReview}
                onReply={reply}
                pendingComment={pendingComment}
                onCloseComment={() => setPendingComment(undefined)}
              />
            </div>
            <div className={tab === "assistant" ? "" : "hidden-panel"}>
              <Assistant
                key={pr.headSHA}
                selection={selection}
                enabled={config.aiEnabled}
                demo={demo}
                onAsk={ask}
              />
            </div>
          </aside>
        </div>
      </main>
    </div>
  );
}
