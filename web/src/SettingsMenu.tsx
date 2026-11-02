import { useEffect, useRef, useState } from "react";
import { SessionManagement } from "./SessionManagement";
import { ThemeToggle } from "./ThemeToggle";
import "./settings-menu.css";
import "./mcp-results.css";
import type { SessionState } from "./session-client";
import type { DiffMode } from "./diff-preference";

export function SettingsMenu({
  session,
  onRetrySession,
  diffMode,
  onDiffMode,
}: {
  session?: SessionState;
  onRetrySession?: () => void;
  diffMode?: DiffMode;
  onDiffMode?: (mode: DiffMode) => void;
} = {}) {
  const menu = useRef<HTMLDetailsElement>(null);
  const trigger = useRef<HTMLElement>(null);
  const [manageSessions, setManageSessions] = useState(false);
  useEffect(() => {
    const dismiss = (event: PointerEvent) => {
      if (
        event.target instanceof Node &&
        menu.current &&
        !menu.current.contains(event.target)
      )
        menu.current.open = false;
    };
    document.addEventListener("pointerdown", dismiss);
    return () => document.removeEventListener("pointerdown", dismiss);
  }, []);
  return (
    <>
      <details
        className="settings-menu"
        ref={menu}
        onKeyDown={(event) => {
          if (event.key === "Escape" && menu.current?.open) {
            event.preventDefault();
            menu.current.open = false;
            trigger.current?.focus();
          }
        }}
        onBlur={(event) => {
          if (
            event.relatedTarget instanceof Node &&
            !event.currentTarget.contains(event.relatedTarget)
          )
            event.currentTarget.open = false;
        }}
      >
        <summary
          className="button settings-trigger"
          ref={trigger}
          aria-label="Settings menu"
          title="Settings"
        >
          <svg
            width="18"
            height="18"
            viewBox="0 0 20 20"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            aria-hidden="true"
          >
            <path d="M3 5h14M3 10h14M3 15h14" />
          </svg>
          <span>Settings</span>
        </summary>
        <div
          className="settings-panel"
          role="group"
          aria-label="Fjord settings"
        >
          <span className="eyebrow">APPEARANCE</span>
          <ThemeToggle />
          <button
            type="button"
            onClick={() => {
              if (menu.current) menu.current.open = false;
              setManageSessions(true);
            }}
          >
            Manage review sessions
          </button>
          {diffMode && onDiffMode && (
            <label className="diff-mode-setting">
              Diff layout
              <select
                value={diffMode}
                onChange={(event) => onDiffMode(event.target.value as DiffMode)}
              >
                <option value="unified">Unified</option>
                <option value="split">Split</option>
              </select>
            </label>
          )}
          {session && session.status !== "disabled" && (
            <section className="mcp-connection" aria-label="MCP connection">
              <span className="eyebrow">AI VIA MCP</span>
              <p role="status">
                {session.status === "connected"
                  ? "Ready for your AI client."
                  : session.status === "disconnected"
                    ? "AI assistance is disconnected."
                    : session.status === "connecting"
                      ? "Preparing review session..."
                      : "Reconnecting review session..."}
              </p>
              <p>
                Add this Streamable HTTP server to your preferred MCP client.
                Your client can read this review, show code, and propose draft
                comments.
              </p>
              <label htmlFor="mcp-server-url">Server URL</label>
              <input
                id="mcp-server-url"
                readOnly
                value={
                  typeof location === "undefined"
                    ? "/mcp"
                    : `${location.origin}/mcp`
                }
                onFocus={(event) => event.currentTarget.select()}
              />
              {session.id && (
                <p className="mcp-session-id">
                  Review session: <code>{session.id}</code>
                </p>
              )}
              {session.error && <p role="alert">{session.error}</p>}
              {(session.status === "retrying" ||
                session.status === "disconnected") && (
                <button onClick={onRetrySession}>
                  {session.status === "disconnected"
                    ? "Reconnect"
                    : "Retry connection"}
                </button>
              )}
              <p>Comments stay drafts until you submit them.</p>
            </section>
          )}
        </div>
      </details>
      {manageSessions && (
        <SessionManagement
          currentId={session?.id}
          onClose={() => {
            setManageSessions(false);
            trigger.current?.focus();
          }}
        />
      )}
    </>
  );
}
