import type { ReviewDraft } from "./ReviewPanel";
import type { DraftComment } from "./types";

export function remainingDraft(
  current: ReviewDraft,
  submitted: ReviewDraft,
): ReviewDraft {
  const sent = new Map(
    submitted.comments.map((comment) => [comment.id, comment.body]),
  );
  return {
    comments: current.comments.filter(
      (comment) => sent.get(comment.id) !== comment.body,
    ),
    body: current.body === submitted.body ? "" : current.body,
    event: current.event === submitted.event ? "COMMENT" : current.event,
  };
}

export function submissionBody(comment: DraftComment): string {
  const attribution = "_AI-assisted draft (Fjord MCP)._";
  return comment.source === "ai" && !comment.body.startsWith(attribution)
    ? `${attribution}\n\n${comment.body}`
    : comment.body;
}
