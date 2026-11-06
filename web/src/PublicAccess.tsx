import { useState, useSyncExternalStore, type ReactNode } from "react";
import {
  authSnapshot,
  connect,
  forgetToken,
  isPublicMode,
  subscribeAuth,
} from "./auth";
import { SettingsMenu } from "./SettingsMenu";
import "./public-access.css";

export function PublicAccess({ children }: { children: ReactNode }) {
  const auth = useSyncExternalStore(subscribeAuth, authSnapshot, authSnapshot);
  const [token, setToken] = useState("");
  if (!isPublicMode() || auth.status === "ready") return children;
  const checking = auth.status === "checking";
  return (
    <div className="public-access">
      <header>
        <a className="public-brand" href="/">
          fjord<span> / code review</span>
        </a>
        <SettingsMenu />
      </header>
      <main>
        <section className="public-intro" aria-labelledby="connect-heading">
          <span className="eyebrow">GITHUB PR REVIEW, WITH CONTEXT</span>
          <h1 id="connect-heading">
            Your code.
            <br />
            <span>A clearer view.</span>
          </h1>
          <p>
            Follow definitions, explore references, and review pull requests
            with the full codebase at hand.
          </p>
          <div className="public-flow" aria-hidden="true">
            <span>DIFF</span>
            <span>&rarr;</span>
            <strong>DEFINITION</strong>
            <span>&rarr;</span>
            <span>REVIEW</span>
          </div>
        </section>
        <section className="token-panel" aria-label="Connect GitHub">
          <h2>Connect your GitHub</h2>
          <p>
            Use a personal access token for the repositories you want to review.
          </p>
          <form
            onSubmit={(event) => {
              event.preventDefault();
              const value = token;
              setToken("");
              void connect(value);
            }}
          >
            <label htmlFor="github-token">GitHub personal access token</label>
            <input
              id="github-token"
              type="password"
              autoComplete="off"
              spellCheck={false}
              autoCapitalize="none"
              value={token}
              onChange={(event) => setToken(event.target.value)}
              placeholder="github_pat_..."
              required
              maxLength={255}
              disabled={checking}
              aria-describedby="token-storage"
            />
            <button className="primary" disabled={checking || !token.trim()}>
              {checking ? "Checking token..." : "Connect GitHub"}
            </button>
            {checking && (
              <button type="button" onClick={() => forgetToken()}>
                Cancel
              </button>
            )}
            {checking && <p role="status">Verifying access with GitHub...</p>}
            {auth.error && (
              <p className="error" role="alert">
                {auth.error}
              </p>
            )}
          </form>
          <p id="token-storage" className="token-disclosure">
            Saved in this browser's local storage and sent to Fjord to call
            GitHub. Only connect on a device you trust. Remove it anytime in
            Settings.
          </p>
          <details className="token-help">
            <summary>Create a token</summary>
            <p>
              Choose only the repositories you need. Allow{" "}
              <strong>Contents: Read</strong> and{" "}
              <strong>Pull requests: Read and write</strong>. Your organization
              may require approval.
            </p>
            <a
              href="https://github.com/settings/personal-access-tokens/new"
              target="_blank"
              rel="noreferrer"
            >
              Create a fine-grained token on GitHub &rarr;
            </a>
          </details>
        </section>
      </main>
    </div>
  );
}
