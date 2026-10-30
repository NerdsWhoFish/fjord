import { expect, it } from "vitest";
import { remainingDraft, submissionBody } from "./review-draft";
import type { ReviewDraft } from "./ReviewPanel";
import type { DraftComment } from "./types";

const comment: DraftComment = {
  id: "human",
  path: "main.go",
  line: 2,
  side: "RIGHT",
  body: "Check this.",
};
const submitted: ReviewDraft = {
  body: "Looks good.",
  event: "APPROVE",
  comments: [comment],
};

it("preserves incoming assistant drafts and changed summary while submission completes", () => {
  const incoming: DraftComment = {
    ...comment,
    id: "ai",
    body: "Potential race.",
    source: "ai",
  };
  expect(
    remainingDraft(
      { ...submitted, body: "Another thought.", comments: [comment, incoming] },
      submitted,
    ),
  ).toEqual({
    body: "Another thought.",
    event: "COMMENT",
    comments: [incoming],
  });
});

it("preserves edits to submitted comments and an explicitly changed decision", () => {
  const edited = { ...comment, body: "Updated explanation." };
  expect(
    remainingDraft(
      { ...submitted, event: "REQUEST_CHANGES", comments: [edited] },
      submitted,
    ),
  ).toEqual({ body: "", event: "REQUEST_CHANGES", comments: [edited] });
});

it("clears only the unchanged submitted review", () => {
  expect(remainingDraft(submitted, submitted)).toEqual({
    body: "",
    event: "COMMENT",
    comments: [],
  });
});

it("preserves AI attribution after editing without duplicating it or labeling human comments", () => {
  expect(submissionBody(comment)).toBe(comment.body);
  const attributed = submissionBody({ ...comment, source: "ai" });
  expect(attributed).toBe("_AI-assisted draft (Fjord MCP)._\n\nCheck this.");
  expect(submissionBody({ ...comment, source: "ai", body: attributed })).toBe(
    attributed,
  );
});
