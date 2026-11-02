import { useId } from "react";
import type { PullRequest, ReviewEvent } from "./types";
import { reviewRestriction } from "./review-policy";

export function ReviewDecisionButton({
  pr,
  event,
  onSelect,
}: {
  pr: PullRequest;
  event: ReviewEvent;
  onSelect: (event: ReviewEvent) => void;
}) {
  const id = useId();
  const reason = reviewRestriction(pr, event);
  const label =
    event === "COMMENT"
      ? "Comment"
      : event === "APPROVE"
        ? "Approve"
        : "Request changes";
  return (
    <span
      className="review-decision-control"
      tabIndex={reason ? 0 : undefined}
      role={reason ? "group" : undefined}
      aria-label={reason ? `${label} unavailable` : undefined}
      aria-describedby={reason ? id : undefined}
    >
      <button
        className={event === "APPROVE" ? "review-approve" : ""}
        disabled={!!reason}
        aria-describedby={reason ? id : undefined}
        onClick={() => onSelect(event)}
      >
        {label}
      </button>
      {reason && (
        <span id={id} role="tooltip" className="review-decision-tooltip">
          {reason}
        </span>
      )}
    </span>
  );
}
