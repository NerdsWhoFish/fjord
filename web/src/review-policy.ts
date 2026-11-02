import type { PullRequest, ReviewEvent } from "./types";

export function reviewRestriction(
  pr: PullRequest,
  event: ReviewEvent,
): string | undefined {
  if (
    event !== "COMMENT" &&
    pr.reviewer &&
    pr.author.toLowerCase() === pr.reviewer.toLowerCase()
  ) {
    return "GitHub doesn't allow you to approve or request changes on your own pull request. Choose Comment to submit your feedback.";
  }
}
