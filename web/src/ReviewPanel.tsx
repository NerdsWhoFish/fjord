import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type {
  DraftComment,
  PullRequest,
  ReviewEvent,
  Selection,
  ThreadComment,
} from "./types";
import { commentLineLabel } from "./comment-range";
import { SyntaxText } from "./SyntaxText";
import { useSyntax } from "./useSyntax";
import type { SyntaxRequest } from "./syntax-types";
import { insertSuggestion, suggestionUnavailable } from "./suggestion";
import "./comment-composer.css";

export interface ReviewDraft {
  comments: DraftComment[];
  body: string;
  event: ReviewEvent;
}
export const emptyDraft = (): ReviewDraft => ({
  comments: [],
  body: "",
  event: "COMMENT",
});

export function editDraftComment(
  draft: ReviewDraft,
  id: string,
  body: string,
): ReviewDraft {
  const text = body.trim();
  if (!text || new TextEncoder().encode(text).length > 65536)
    throw new Error("Comments must contain text and fit within 64 KiB.");
  if (!draft.comments.some((comment) => comment.id === id))
    throw new Error("This draft comment no longer exists.");
  return {
    ...draft,
    comments: draft.comments.map((comment) =>
      comment.id === id ? { ...comment, body: text } : comment,
    ),
  };
}

export function canSubmitReview(
  pr: PullRequest,
  demo: boolean,
  draft: ReviewDraft,
) {
  if (demo || pr.state !== "open") return false;
  if (draft.event === "REQUEST_CHANGES") return !!draft.body.trim();
  if (draft.event === "COMMENT")
    return !!draft.body.trim() || draft.comments.length > 0;
  return draft.event === "APPROVE";
}

export function draftKey(pr: PullRequest, demo: boolean) {
  return `fjord:${demo ? "demo:" : ""}${pr.owner}/${pr.repo}/${pr.number}/${pr.baseSHA}/${pr.headSHA}`;
}

export function readDraft(pr: PullRequest, demo: boolean): ReviewDraft {
  try {
    const saved = JSON.parse(
      sessionStorage.getItem(draftKey(pr, demo)) || "null",
    );
    if (
      saved &&
      Array.isArray(saved.comments) &&
      typeof saved.body === "string" &&
      ["COMMENT", "APPROVE", "REQUEST_CHANGES"].includes(saved.event)
    )
      return saved;
  } catch {
    /* A browser can disable storage; reviews still work in memory. */
  }
  return emptyDraft();
}

export function writeDraft(
  pr: PullRequest,
  demo: boolean,
  draft: ReviewDraft,
): boolean {
  try {
    sessionStorage.setItem(draftKey(pr, demo), JSON.stringify(draft));
    return true;
  } catch {
    return false;
  }
}

interface Props {
  pr: PullRequest;
  demo: boolean;
  draft: ReviewDraft;
  onDraft: (draft: ReviewDraft) => void;
  threads: ThreadComment[];
  threadsError?: string;
  onReloadThreads: () => void;
  onSubmit: () => Promise<void>;
  onReply: (id: number, body: string) => Promise<void>;
  pendingComment?: Selection;
  onCloseComment: () => void;
  open: boolean;
  onCloseReview: () => void;
}

export function ReviewPanel({
  pr,
  demo,
  draft,
  onDraft,
  threads,
  threadsError,
  onReloadThreads,
  onSubmit,
  onReply,
  pendingComment,
  onCloseComment,
  open,
  onCloseReview,
}: Props) {
  const [comment, setComment] = useState("");
  const [reply, setReply] = useState<Record<number, string>>({});
  const [sending, setSending] = useState(false);
  const [replying, setReplying] = useState<number>();
  const [error, setError] = useState("");
  const [editing, setEditing] = useState<{ id: string; body: string }>();
  const commentDialog = useRef<HTMLDialogElement>(null);
  const commentInput = useRef<HTMLTextAreaElement>(null);
  const suggestionSelection = useRef<
    { start: number; end: number } | undefined
  >(undefined);
  const submitDialog = useRef<HTMLDialogElement>(null);
  const previewRequest = useMemo<SyntaxRequest | undefined>(
    () =>
      pendingComment
        ? {
            kind: "source",
            path: pendingComment.path,
            code: pendingComment.code,
          }
        : undefined,
    [pendingComment?.path, pendingComment?.code],
  );
  const previewSyntax = useSyntax(previewRequest);
  const previewLines = pendingComment?.code.split("\n") || [];
  const suggestionReason = suggestionUnavailable(pendingComment);
  useLayoutEffect(() => {
    const selected = suggestionSelection.current;
    if (!selected || !commentInput.current) return;
    suggestionSelection.current = undefined;
    commentInput.current.focus();
    commentInput.current.setSelectionRange(selected.start, selected.end);
  }, [comment]);
  useEffect(() => {
    suggestionSelection.current = undefined;
    setComment("");
    if (pendingComment) commentDialog.current?.showModal();
    else commentDialog.current?.close();
  }, [pendingComment]);
  useEffect(() => {
    if (open) {
      setError("");
      submitDialog.current?.showModal();
    } else submitDialog.current?.close();
  }, [open]);
  async function send() {
    setSending(true);
    setError("");
    try {
      await onSubmit();
      submitDialog.current?.close();
    } catch (error) {
      setError(
        error instanceof Error
          ? error.message
          : "Review could not be submitted.",
      );
    } finally {
      setSending(false);
    }
  }
  async function sendReply(id: number) {
    setReplying(id);
    setError("");
    try {
      await onReply(id, reply[id]);
      setReply((current) => ({ ...current, [id]: "" }));
    } catch (error) {
      setError(
        error instanceof Error ? error.message : "Reply could not be sent.",
      );
    } finally {
      setReplying(undefined);
    }
  }
  return (
    <>
      <dialog
        ref={submitDialog}
        className="review-summary-dialog"
        aria-labelledby="submit-title"
        onClose={onCloseReview}
        onCancel={(event) => {
          if (sending) event.preventDefault();
        }}
      >
        <div className="review-panel">
          <div className="dialog-heading">
            <h2 id="submit-title">
              Review {pr.owner}/{pr.repo}#{pr.number}
            </h2>
            <button
              className="icon-button"
              disabled={sending}
              aria-label="Close review summary"
              onClick={onCloseReview}
            >
              x
            </button>
          </div>
          <div className="section-title">
            <span className="eyebrow">YOUR REVIEW</span>
            <span className="count">
              {draft.comments.length}{" "}
              {draft.comments.length === 1 ? "draft" : "drafts"}
            </span>
          </div>
          <p className="muted small">
            Drafts stay in this browser tab until you submit. They are tied to
            this exact PR revision ({pr.headSHA.slice(0, 7)}). Fjord checks that
            the PR has not changed before submitting.
          </p>
          {draft.comments.map((item) => (
            <article className="draft-card" key={item.id}>
              <div className="draft-location">
                <code>
                  {item.path}:{commentLineLabel(item.line, item.startLine)}
                </code>
                <span>{item.side === "LEFT" ? "base" : "head"}</span>
              </div>
              {editing?.id === item.id ? (
                <div className="draft-editor">
                  <label htmlFor={`draft-edit-${item.id}`}>
                    Edit draft comment
                  </label>
                  <textarea
                    id={`draft-edit-${item.id}`}
                    rows={6}
                    value={editing.body}
                    disabled={sending}
                    onChange={(event) =>
                      setEditing({ id: item.id, body: event.target.value })
                    }
                  />
                  <div className="dialog-actions">
                    <button
                      disabled={sending}
                      onClick={() => setEditing(undefined)}
                    >
                      Cancel edit
                    </button>
                    <button
                      disabled={sending || !editing.body.trim()}
                      onClick={() => {
                        try {
                          onDraft(
                            editDraftComment(draft, item.id, editing.body),
                          );
                          setEditing(undefined);
                          setError("");
                        } catch (cause) {
                          setError(
                            cause instanceof Error
                              ? cause.message
                              : "Could not edit this draft.",
                          );
                        }
                      }}
                    >
                      Save draft
                    </button>
                  </div>
                </div>
              ) : (
                <p>{item.body}</p>
              )}
              <div className="draft-actions">
                {item.source === "ai" && (
                  <span className="draft-attribution">Suggested by AI</span>
                )}
                {editing?.id !== item.id && (
                  <button
                    className="text-button"
                    disabled={sending}
                    onClick={() => setEditing({ id: item.id, body: item.body })}
                  >
                    Edit draft
                  </button>
                )}
                <button
                  className="text-button"
                  disabled={sending}
                  onClick={() => {
                    if (editing?.id === item.id) setEditing(undefined);
                    onDraft({
                      ...draft,
                      comments: draft.comments.filter((c) => c.id !== item.id),
                    });
                  }}
                >
                  Remove draft
                </button>
              </div>
            </article>
          ))}
          <label htmlFor="review-summary">Review summary</label>
          <textarea
            id="review-summary"
            rows={5}
            disabled={sending}
            value={draft.body}
            onChange={(e) => onDraft({ ...draft, body: e.target.value })}
            placeholder="What should the author know?"
          />
          <label htmlFor="review-decision">Review decision</label>
          <select
            id="review-decision"
            value={draft.event}
            disabled={sending}
            onChange={(e) =>
              onDraft({ ...draft, event: e.target.value as ReviewEvent })
            }
          >
            <option value="COMMENT">Comment</option>
            <option value="APPROVE">Approve</option>
            <option value="REQUEST_CHANGES">Request changes</option>
          </select>
          <button
            className="primary wide"
            disabled={sending || !!editing || !canSubmitReview(pr, demo, draft)}
            onClick={send}
          >
            {sending ? "Submitting..." : "Submit review to GitHub"}
          </button>
          {demo && (
            <p className="small demo-note">
              Demo review only. Nothing can be sent to GitHub.
            </p>
          )}
          {!demo && pr.state !== "open" && (
            <p className="notice">
              This pull request is closed. You can read its changes and
              conversation, but cannot submit a review.
            </p>
          )}
          {error && (
            <p className="error" role="alert">
              {error}
            </p>
          )}
          <div className="section-title thread-title">
            <span className="eyebrow">CONVERSATION</span>
            <button className="text-button" onClick={onReloadThreads}>
              Refresh
            </button>
          </div>
          {threadsError ? (
            <p className="error" role="alert">
              {threadsError}
            </p>
          ) : (
            !threads.length && (
              <p className="muted small">No inline review comments yet.</p>
            )
          )}
          {threads.map((thread) => (
            <article className="thread" key={thread.id}>
              <div className="thread-author">
                <span className="avatar">
                  {thread.author.slice(0, 1).toUpperCase()}
                </span>
                <strong>{thread.author}</strong>
                {thread.url && (
                  <a href={thread.url} target="_blank" rel="noreferrer">
                    GitHub
                  </a>
                )}
              </div>
              <code className="thread-path">
                {thread.path}
                {thread.line
                  ? `:${commentLineLabel(thread.line, thread.startLine)}`
                  : " (outdated)"}
              </code>
              <p>{thread.body}</p>
              <label className="sr-only" htmlFor={`reply-${thread.id}`}>
                Reply to {thread.author}
              </label>
              <textarea
                id={`reply-${thread.id}`}
                rows={2}
                placeholder="Write a reply..."
                value={reply[thread.id] || ""}
                onChange={(e) =>
                  setReply((current) => ({
                    ...current,
                    [thread.id]: e.target.value,
                  }))
                }
              />
              <button
                disabled={
                  demo || replying !== undefined || !reply[thread.id]?.trim()
                }
                onClick={() => sendReply(thread.id)}
              >
                {replying === thread.id ? "Sending..." : "Send reply"}
              </button>
            </article>
          ))}
          <div className="dialog-actions">
            <button disabled={sending} onClick={onCloseReview}>
              Keep reviewing
            </button>
          </div>
        </div>
      </dialog>
      <dialog
        ref={commentDialog}
        className="comment-composer"
        onCancel={onCloseComment}
        onClose={onCloseComment}
        aria-labelledby="comment-title"
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!pendingComment || !comment.trim()) return;
            onDraft({
              ...draft,
              comments: [
                ...draft.comments,
                {
                  // getRandomValues also works over HTTP on a trusted LAN.
                  id: Array.from(
                    crypto.getRandomValues(new Uint32Array(4)),
                    (n) => n.toString(16).padStart(8, "0"),
                  ).join(""),
                  path:
                    pendingComment.side === "LEFT"
                      ? pr.files.find(
                          (f) => f.previousPath === pendingComment.path,
                        )?.path || pendingComment.path
                      : pendingComment.path,
                  line: pendingComment.line,
                  side: pendingComment.side,
                  ...(pendingComment.startLine !== undefined
                    ? {
                        startLine: pendingComment.startLine,
                        startSide:
                          pendingComment.startSide || pendingComment.side,
                      }
                    : {}),
                  body: comment.trim(),
                },
              ],
            });
            onCloseComment();
          }}
        >
          <div className="dialog-heading">
            <h2 id="comment-title">
              {pendingComment?.startLine
                ? "Comment on selected lines"
                : "Leave a line comment"}
            </h2>
            <button
              type="button"
              className="icon-button"
              onClick={onCloseComment}
              aria-label="Close comment"
            >
              x
            </button>
          </div>
          <p className="muted small">
            {pendingComment?.path}:
            {pendingComment
              ? commentLineLabel(pendingComment.line, pendingComment.startLine)
              : ""}{" "}
            / {pendingComment?.side === "LEFT" ? "base" : "head"}
          </p>
          <pre
            className="selected-code"
            tabIndex={0}
            role="region"
            aria-label="Selected code for comment"
          >
            <code>
              {previewLines.map((line, index) => (
                <span key={index}>
                  <SyntaxText text={line} spans={previewSyntax.lines[index]} />
                  {index < previewLines.length - 1 ? "\n" : ""}
                </span>
              ))}
            </code>
          </pre>
          {previewSyntax.limited && (
            <p className="composer-help">
              Syntax highlighting is unavailable for this selection.
            </p>
          )}
          <div className="comment-editor-heading">
            <label htmlFor="line-comment">Comment</label>
            <button
              type="button"
              className="suggestion-button"
              disabled={!!suggestionReason}
              aria-describedby={
                suggestionReason ? "suggestion-unavailable" : undefined
              }
              onClick={() => {
                if (
                  !pendingComment ||
                  suggestionReason ||
                  !commentInput.current
                )
                  return;
                const inserted = insertSuggestion(
                  comment,
                  pendingComment.code,
                  commentInput.current.selectionStart,
                  commentInput.current.selectionEnd,
                );
                suggestionSelection.current = {
                  start: inserted.codeStart,
                  end: inserted.codeEnd,
                };
                setComment(inserted.body);
              }}
            >
              <svg
                width="16"
                height="16"
                viewBox="0 0 16 16"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.4"
                aria-hidden="true"
              >
                <path d="M5 4 1 8l4 4M11 4l4 4-4 4M9 2 7 14" />
              </svg>
              Insert suggestion
            </button>
          </div>
          {suggestionReason && (
            <p id="suggestion-unavailable" className="composer-help">
              {suggestionReason}
            </p>
          )}
          <textarea
            id="line-comment"
            ref={commentInput}
            autoFocus
            rows={9}
            value={comment}
            onChange={(e) => setComment(e.target.value)}
            aria-describedby="comment-markdown-help"
          />
          <p id="comment-markdown-help" className="composer-help">
            Markdown supported. Edit the code inside a suggestion to propose a
            replacement.
          </p>
          <div className="dialog-actions">
            <button type="button" onClick={onCloseComment}>
              Cancel
            </button>
            <button className="primary" disabled={!comment.trim()}>
              Add to review
            </button>
          </div>
        </form>
      </dialog>
    </>
  );
}
