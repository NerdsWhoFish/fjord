import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { demoPR } from "./demo";
import { ReviewDecisionButton } from "./ReviewDecisionButton";
import { canSubmitReview, emptyDraft } from "./ReviewPanel";
import { reviewRestriction } from "./review-policy";

describe("self review", () => {
  const ownPR = { ...demoPR, state: "open", author: "Joey", reviewer: "joey" };

  it("blocks decisions on your own PR while preserving comment submission", () => {
    for (const event of ["REQUEST_CHANGES", "APPROVE"] as const) {
      expect(reviewRestriction(ownPR, event)).toContain(
        "your own pull request",
      );
      expect(
        canSubmitReview(ownPR, false, {
          ...emptyDraft(),
          event,
          body: "Feedback",
        }),
      ).toBe(false);
    }
    expect(
      canSubmitReview(ownPR, false, { ...emptyDraft(), body: "Feedback" }),
    ).toBe(true);
  });

  it("does not infer identity when it is unknown or belongs to another reviewer", () => {
    for (const reviewer of [undefined, "", "someone-else"]) {
      expect(
        reviewRestriction({ ...ownPR, reviewer }, "REQUEST_CHANGES"),
      ).toBeUndefined();
    }
  });

  it("provides a focusable explanation around the disabled decision", () => {
    const html = renderToStaticMarkup(
      <ReviewDecisionButton
        pr={ownPR}
        event="REQUEST_CHANGES"
        onSelect={() => {}}
      />,
    );
    expect(html).toContain("disabled");
    expect(html).toContain('tabindex="0"');
    expect(html).toContain('role="tooltip"');
    expect(html).toContain("Choose Comment");
    const other = renderToStaticMarkup(
      <ReviewDecisionButton
        pr={{ ...ownPR, reviewer: "Other" }}
        event="REQUEST_CHANGES"
        onSelect={() => {}}
      />,
    );
    expect(other).not.toContain("disabled");
    expect(other).not.toContain('role="tooltip"');
  });
});
