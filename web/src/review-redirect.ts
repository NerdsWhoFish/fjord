import { githubPRURL } from "./pr-route";
import type { PullRequest } from "./types";

export function reviewRedirect(
  pr: PullRequest,
  submittedEpoch: number,
  currentEpoch: number,
  demo: boolean,
): string | undefined {
  if (demo || submittedEpoch !== currentEpoch) return;
  return githubPRURL(pr);
}
