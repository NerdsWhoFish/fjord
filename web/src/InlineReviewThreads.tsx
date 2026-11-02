import { useState } from "react";
import type { ReviewThread } from "./diff-layout";
import type { DraftComment } from "./types";
import { MarkdownContent } from "./MarkdownContent";
import { MarkdownEditor } from "./MarkdownEditor";

export function InlineDraftComments({
  drafts,
  onReviewDrafts,
}: {
  drafts: DraftComment[];
  onReviewDrafts?: () => void;
}) {
  return (
    <>
      {drafts.map((draft) => (
        <section
          key={draft.id}
          className="inline-review-thread inline-draft-comment"
          aria-label={`Draft on ${draft.side === "LEFT" ? "base" : "head"} line ${draft.line}`}
        >
          <header>
            Draft{draft.source === "ai" ? " / AI suggestion" : ""} /{" "}
            {draft.side === "LEFT" ? "Base" : "Head"}{" "}
            {draft.startLine
              ? `lines ${draft.startLine}-${draft.line}`
              : `line ${draft.line}`}
          </header>
          <MarkdownContent>{draft.body}</MarkdownContent>
          {onReviewDrafts && (
            <button type="button" onClick={onReviewDrafts}>
              Edit draft
            </button>
          )}
        </section>
      ))}
    </>
  );
}

export function InlineReviewThreads({
  threads,
  onReply,
}: {
  threads: ReviewThread[];
  onReply?: (commentId: number, body: string) => Promise<void>;
}) {
  return (
    <>
      {threads.map((thread) => (
        <InlineThread key={thread.root.id} thread={thread} onReply={onReply} />
      ))}
    </>
  );
}

function InlineThread({
  thread,
  onReply,
}: {
  thread: ReviewThread;
  onReply?: (commentId: number, body: string) => Promise<void>;
}) {
  const [body, setBody] = useState("");
  const [editing, setEditing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const { root } = thread;
  return (
    <section
      className="inline-review-thread"
      aria-label={`Review thread on ${root.side === "LEFT" ? "base" : "head"} ${root.startLine ? `lines ${root.startLine}-${root.line}` : `line ${root.line}`}`}
    >
      <header>
        {root.side === "LEFT" ? "Base" : "Head"}{" "}
        {root.startLine
          ? `lines ${root.startLine}-${root.line}`
          : `line ${root.line}`}
      </header>
      {thread.comments.map((comment) => (
        <article key={comment.id}>
          <strong>{comment.author}</strong>
          <MarkdownContent>{comment.body}</MarkdownContent>
        </article>
      ))}
      {onReply && !editing && (
        <button type="button" onClick={() => setEditing(true)}>
          Reply
        </button>
      )}
      {onReply && editing && (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            if (busy || !body.trim()) return;
            setBusy(true);
            setError("");
            void onReply(root.id, body.trim())
              .then(() => {
                setBody("");
                setEditing(false);
              })
              .catch((failure: unknown) =>
                setError(
                  failure instanceof Error
                    ? failure.message
                    : "Reply outcome unknown. Your text is preserved. Check GitHub before retrying.",
                ),
              )
              .finally(() => setBusy(false));
          }}
        >
          <label htmlFor={`inline-reply-${root.id}`}>
            Reply to {root.author}
          </label>
          <MarkdownEditor
            id={`inline-reply-${root.id}`}
            label={`Reply to ${root.author}`}
            value={body}
            onChange={(event) => setBody(event.target.value)}
            disabled={busy}
            rows={3}
            required
          />
          {error && <p role="alert">{error}</p>}
          <div className="inline-thread-actions">
            <button type="submit" disabled={busy || !body.trim()}>
              {busy ? "Posting..." : "Post reply"}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={() => setEditing(false)}
            >
              Cancel
            </button>
          </div>
        </form>
      )}
    </section>
  );
}
