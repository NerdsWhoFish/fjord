import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { captureError } from "./telemetry";
import "./session-management.css";

export interface ReviewSessionSummary {
  id: string;
  owner: string;
  repo: string;
  number: number;
  title: string;
  headSHA: string;
  updatedAt: string;
  expiresAt: string;
  draftCommentCount?: number;
}

export function isSessionSummary(
  value: unknown,
): value is ReviewSessionSummary {
  if (!value || typeof value !== "object") return false;
  const item = value as Partial<ReviewSessionSummary>;
  return (
    ["id", "owner", "repo", "title", "headSHA", "updatedAt", "expiresAt"].every(
      (key) => typeof item[key as keyof ReviewSessionSummary] === "string",
    ) &&
    Number.isSafeInteger(item.number) &&
    Number(item.number) > 0 &&
    Number.isFinite(Date.parse(item.updatedAt!)) &&
    Number.isFinite(Date.parse(item.expiresAt!)) &&
    (item.draftCommentCount === undefined ||
      (Number.isSafeInteger(item.draftCommentCount) &&
        item.draftCommentCount >= 0))
  );
}

export function sessionAge(timestamp: string, now: number): string {
  const date = Date.parse(timestamp);
  if (!Number.isFinite(date)) return "Unknown";
  const minutes = Math.max(0, Math.floor((now - date) / 60_000));
  if (minutes < 1) return "Just now";
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return hours < 24 ? `${hours}h ago` : `${Math.floor(hours / 24)}d ago`;
}

export function SessionManagement({
  currentId,
  onClose,
}: {
  currentId?: string;
  onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const lifetime = useRef(new AbortController());
  const refreshButton = useRef<HTMLButtonElement>(null);
  const confirmButton = useRef<HTMLButtonElement>(null);
  const [sessions, setSessions] = useState<ReviewSessionSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [removing, setRemoving] = useState<string>();
  const [confirming, setConfirming] = useState<string>();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (notice && !removing) refreshButton.current?.focus();
  }, [notice, removing]);
  useEffect(() => {
    if (confirming) confirmButton.current?.focus();
  }, [confirming]);

  async function refresh(signal: AbortSignal) {
    setLoading(true);
    setError("");
    try {
      const result = await api<{ sessions: ReviewSessionSummary[] }>(
        "session/list",
        {},
        AbortSignal.any([signal, AbortSignal.timeout(15_000)]),
      );
      if (
        !Array.isArray(result.sessions) ||
        !result.sessions.every(isSessionSummary)
      )
        throw new Error("Invalid session list response.");
      if (!signal.aborted) {
        setSessions(result.sessions);
        setNow(Date.now());
      }
    } catch (cause) {
      if (!signal.aborted) {
        setError("Couldn't load sessions. Try refreshing.");
        captureError(cause, "session.management");
      }
    } finally {
      if (!signal.aborted) setLoading(false);
    }
  }

  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    dialog.current?.showModal();
    void refresh(controller.signal);
    const timer = window.setInterval(() => setNow(Date.now()), 30_000);
    return () => {
      controller.abort();
      clearInterval(timer);
      dialog.current?.close();
    };
  }, []);

  async function remove(id: string) {
    if (id === currentId || removing) return;
    setRemoving(id);
    setError("");
    setNotice("");
    const signal = lifetime.current.signal;
    try {
      await api(
        "session/remove",
        { id },
        AbortSignal.any([signal, AbortSignal.timeout(15_000)]),
      );
      if (!signal.aborted) {
        setSessions((items) => items.filter((item) => item.id !== id));
        setConfirming(undefined);
        setNotice("Session removed.");
      }
    } catch (cause) {
      if (!signal.aborted) {
        setError("Couldn't confirm removal. Refresh the list before retrying.");
        captureError(cause, "session.management");
      }
    } finally {
      if (!signal.aborted) setRemoving(undefined);
    }
  }

  const ordered = [...sessions].sort(
    (a, b) =>
      Number(b.id === currentId) - Number(a.id === currentId) ||
      Date.parse(b.updatedAt) - Date.parse(a.updatedAt) ||
      a.id.localeCompare(b.id),
  );
  return (
    <dialog
      ref={dialog}
      className="session-management"
      aria-labelledby="sessions-title"
      aria-describedby="sessions-description"
      onCancel={(event) => {
        event.preventDefault();
        onClose();
      }}
    >
      <div className="dialog-heading">
        <div>
          <span className="eyebrow">SETTINGS / AI ASSISTANCE</span>
          <h2 id="sessions-title">Review sessions</h2>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close session management"
        >
          Close
        </button>
      </div>
      <p id="sessions-description">
        Sessions visible to your MCP client. Removing one disconnects AI
        assistance and discards pending AI actions. Browser drafts and GitHub
        comments stay intact.
      </p>
      <div className="session-list-toolbar">
        <span>
          {loading
            ? "Loading sessions..."
            : `${sessions.length} review session${sessions.length === 1 ? "" : "s"}`}
        </span>
        <button
          ref={refreshButton}
          type="button"
          disabled={loading || !!removing}
          onClick={() => void refresh(lifetime.current.signal)}
        >
          Refresh
        </button>
      </div>
      {error && <p role="alert">{error}</p>}
      <p role="status" className="session-notice">
        {notice}
      </p>
      {!loading && !error && sessions.length === 0 && (
        <p className="session-empty">
          No review sessions. Open a PR to connect it to your MCP client.
        </p>
      )}
      <ul
        className="session-list"
        aria-label="Review sessions"
        aria-busy={loading}
      >
        {ordered.map((session) => {
          const current = session.id === currentId;
          return (
            <li
              key={session.id}
              className={current ? "session-current" : undefined}
            >
              <div className="session-row-heading">
                <div>
                  <span className="eyebrow">
                    {session.owner}/{session.repo} #{session.number}
                  </span>
                  <h3>{session.title}</h3>
                </div>
                {current ? (
                  <span className="session-current-label">This tab</span>
                ) : (
                  <button
                    type="button"
                    disabled={loading || !!removing}
                    aria-label={`Remove session ${session.id}`}
                    onClick={() => setConfirming(session.id)}
                  >
                    Remove
                  </button>
                )}
              </div>
              <div className="session-metadata">
                <span>
                  Last synced{" "}
                  <time
                    dateTime={session.updatedAt}
                    title={new Date(session.updatedAt).toLocaleString()}
                  >
                    {sessionAge(session.updatedAt, now)}
                  </time>
                </span>
                <span>
                  Revision <code>{session.headSHA.slice(0, 7)}</code>
                </span>
                {!!session.draftCommentCount && (
                  <span>{session.draftCommentCount} draft comments</span>
                )}
              </div>
              <p className="session-id">
                Session <code>{session.id}</code>
              </p>
              {confirming === session.id && !current && (
                <div className="session-remove-confirmation">
                  <p>
                    This tab will need to reconnect before an AI client can use
                    it again.
                  </p>
                  <div>
                    <button
                      ref={confirmButton}
                      type="button"
                      disabled={!!removing}
                      onClick={() => void remove(session.id)}
                    >
                      {removing === session.id
                        ? "Removing..."
                        : "Confirm removal"}
                    </button>
                    <button
                      type="button"
                      disabled={!!removing}
                      onClick={() => setConfirming(undefined)}
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              )}
            </li>
          );
        })}
      </ul>
      <p className="session-help">
        Background tabs keep syncing. Inactive sessions expire after two hours.
        Your current session is marked &quot;This tab&quot;.
      </p>
    </dialog>
  );
}
