import { parsePRRoute } from "./pr-route";
import "./pr-loading.css";

export function PRLoading({ url }: { url: string }) {
  const route = parsePRRoute({
    pathname: "/",
    search: `?${new URLSearchParams({ pr: url.trim() })}`,
    hash: "",
  });
  const label =
    route.kind === "pr"
      ? `${route.target.owner}/${route.target.repo} #${route.target.number}`
      : "Your GitHub review";
  return (
    <main className="pr-loading" aria-busy="true">
      <div className="pr-loading-copy" role="status" aria-live="polite">
        <span className="eyebrow">FOLLOW THE CODE</span>
        <h1>Opening pull request</h1>
        <p className="pr-loading-target">{label}</p>
        <p>Loading pull request details and changed files from GitHub.</p>
      </div>
      <div className="pr-loading-preview" aria-hidden="true">
        <span />
        <span />
        <span />
        <span />
        <span />
      </div>
      <a className="text-button" href="/">
        Cancel and return home
      </a>
    </main>
  );
}
