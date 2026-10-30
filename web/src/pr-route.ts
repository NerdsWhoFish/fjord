export interface PRTarget {
  owner: string;
  repo: string;
  number: number;
}

export type PRRoute =
  | { kind: "home" }
  | { kind: "pr"; target: PRTarget; githubURL: string; path: string }
  | { kind: "error"; message: string };

const ownerPattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const repoPattern = /^[A-Za-z0-9_.-]{1,100}$/;
const reserved = new Set(["api", "assets", "mcp"]);

function target(owner: string, repo: string, rawNumber: string): PRTarget | undefined {
  const number = Number(rawNumber);
  if (
    !ownerPattern.test(owner) ||
    reserved.has(owner.toLowerCase()) ||
    !repoPattern.test(repo) ||
    repo === "." ||
    repo === ".." ||
    !/^\d+$/.test(rawNumber) ||
    !Number.isSafeInteger(number) ||
    number < 1
  ) return;
  return { owner, repo, number };
}

export function prPath(pr: PRTarget): string {
  return `/${pr.owner}/${pr.repo}/${pr.number}`;
}

export function githubPRURL(pr: PRTarget): string {
  return `https://github.com/${pr.owner}/${pr.repo}/pull/${pr.number}`;
}

function githubTarget(raw: string): PRTarget | undefined {
  try {
    const url = new URL(raw);
    if (url.origin !== "https://github.com" || url.username || url.password ||
      url.search || url.hash || raw.includes("?") || raw.includes("#")) return;
    const match = /^\/([^/]+)\/([^/]+)\/pull\/(\d+)(?:\/(?:changes|files))?\/?$/.exec(url.pathname);
    return match ? target(match[1], match[2], match[3]) : undefined;
  } catch {
    return;
  }
}

export function parsePRRoute(location: Pick<Location, "pathname" | "search" | "hash">): PRRoute {
  const invalid: PRRoute = {
    kind: "error",
    message: "Use /owner/repo/number, ?pr=https://github.com/owner/repo/pull/number, or ?owner=owner&repo=repo&number=number.",
  };
  if (location.hash) return invalid;
  const params = new URLSearchParams(location.search);
  const entries = [...params.keys()];
  if (entries.some((key) => !["pr", "owner", "repo", "number"].includes(key)) ||
    new Set(entries).size !== entries.length) return invalid;
  let selected: PRTarget | undefined;
  if (location.pathname !== "/") {
    if (entries.length) return invalid;
    const match = /^\/([^/]+)\/([^/]+)\/(\d+)\/?$/.exec(location.pathname);
    if (!match) return invalid;
    selected = target(match[1], match[2], match[3]);
  } else if (!entries.length) {
    return { kind: "home" };
  } else if (entries.length === 1 && params.has("pr")) {
    selected = githubTarget(params.get("pr")!);
  } else if (entries.length === 3 && ["owner", "repo", "number"].every((key) => params.has(key))) {
    selected = target(params.get("owner")!, params.get("repo")!, params.get("number")!);
  }
  return selected
    ? { kind: "pr", target: selected, githubURL: githubPRURL(selected), path: prPath(selected) }
    : invalid;
}
