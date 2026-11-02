import type { ConversationEntry, PullRequest } from "./types";
import "./conversation.css";
import { MarkdownContent } from "./MarkdownContent";

export function reviewLabel(state?: string) {
  switch (state) {
    case "APPROVED":
      return "Approved";
    case "CHANGES_REQUESTED":
      return "Requested changes";
    case "DISMISSED":
      return "Review dismissed";
    default:
      return "Reviewed";
  }
}

export function ConversationTimeline({
  pr,
  entries,
}: {
  pr: PullRequest;
  entries: ConversationEntry[];
}) {
  return (
    <div
      className="conversation-timeline"
      aria-label="Pull request conversation"
    >
      {pr.body && (
        <details className="thread pr-description">
          <summary>Pull request description by {pr.author}</summary>
          <MarkdownContent>{pr.body}</MarkdownContent>
        </details>
      )}
      {entries.map((entry) => (
        <article className="thread" key={`${entry.kind}:${entry.id}`}>
          <div className="thread-author">
            <span className="avatar" aria-hidden="true">
              {entry.author.slice(0, 1).toUpperCase()}
            </span>
            <strong>{entry.author}</strong>
            <span className="conversation-event">
              {entry.kind === "review" ? reviewLabel(entry.state) : "Commented"}
            </span>
            {entry.url?.startsWith("https://github.com/") && (
              <a href={entry.url} target="_blank" rel="noreferrer">
                GitHub
              </a>
            )}
          </div>
          {entry.createdAt && (
            <time dateTime={entry.createdAt}>
              {new Date(entry.createdAt).toLocaleString()}
            </time>
          )}
          {entry.body && <MarkdownContent>{entry.body}</MarkdownContent>}
        </article>
      ))}
    </div>
  );
}
