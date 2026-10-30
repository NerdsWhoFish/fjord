import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { activeOutlineLocation, OutlinePanel } from "./OutlinePanel";
import { searchableSymbols, WorkspaceSearch } from "./WorkspaceSearch";
import { canSubmitReview, emptyDraft, ReviewPanel } from "./ReviewPanel";
import { demoPR } from "./demo";
import type { ReviewItem, Selection } from "./types";

const selected: Selection = {
  path: "main.go",
  sha: "head",
  side: "RIGHT",
  line: 1,
  character: 0,
  code: "",
  word: "",
};
const items: ReviewItem[] = [
  {
    label: "Other",
    location: {
      path: "other.go",
      line: 15,
      character: 0,
      endLine: 15,
      endCharacter: 5,
    },
  },
  {
    label: "Parent",
    location: {
      path: "main.go",
      line: 3,
      character: 0,
      endLine: 3,
      endCharacter: 6,
    },
    children: [
      {
        label: "Nested",
        location: {
          path: "main.go",
          line: 10,
          character: 0,
          endLine: 10,
          endCharacter: 6,
        },
      },
    ],
  },
  {
    label: "Next",
    location: {
      path: "main.go",
      line: 20,
      character: 0,
      endLine: 20,
      endCharacter: 4,
    },
  },
];
const inspect = async () => ({ locations: [], hover: "", items });

describe("outline and heading symbol search", () => {
  it("highlights the nearest preceding symbol in the current file", () => {
    expect(activeOutlineLocation(items, "main.go", 1)).toBeUndefined();
    expect(activeOutlineLocation(items, "main.go", 6)?.line).toBe(3);
    expect(activeOutlineLocation(items, "main.go", 18)?.line).toBe(10);
    expect(activeOutlineLocation(items, "main.go", 23)?.line).toBe(20);
    expect(activeOutlineLocation(items, "unknown.go", 23)).toBeUndefined();
  });
  it("keeps nested workspace targets discoverable", () => {
    expect(
      searchableSymbols([{ label: "group", children: items }]).map(
        (item) => item.label,
      ),
    ).toEqual(["Other", "Parent", "Nested", "Next"]);
  });
  it("shows revision-specific loading and keyboard search affordances", () => {
    const outline = renderToStaticMarkup(
      <OutlinePanel
        selected={selected}
        inspect={inspect}
        onLocation={() => {}}
      />,
    );
    expect(outline).toContain("Loading file outline...");
    expect(outline).toContain("Head / head");
    const search = renderToStaticMarkup(
      <WorkspaceSearch
        selected={selected}
        inspect={inspect}
        onLocation={() => {}}
      />,
    );
    expect(search).toContain('aria-keyshortcuts="Meta+K Control+K"');
    expect(search).toContain("including unchanged files");
  });
});

describe("review summary dialog", () => {
  const pr = { ...demoPR, state: "open" };
  it("requires explanation for changes, content for comments, and open non-demo PRs", () => {
    expect(canSubmitReview(pr, false, emptyDraft())).toBe(false);
    expect(canSubmitReview(pr, false, { ...emptyDraft(), body: " " })).toBe(
      false,
    );
    expect(
      canSubmitReview(pr, false, { ...emptyDraft(), event: "APPROVE" }),
    ).toBe(true);
    expect(
      canSubmitReview(pr, false, { ...emptyDraft(), event: "REQUEST_CHANGES" }),
    ).toBe(false);
    expect(
      canSubmitReview(pr, false, {
        ...emptyDraft(),
        event: "REQUEST_CHANGES",
        body: "Please handle cancellation.",
      }),
    ).toBe(true);
    expect(
      canSubmitReview({ ...pr, state: "closed" }, false, {
        ...emptyDraft(),
        event: "APPROVE",
      }),
    ).toBe(false);
    expect(
      canSubmitReview(pr, true, { ...emptyDraft(), event: "APPROVE" }),
    ).toBe(false);
  });
  it("holds editable summary and decisions together with one explicit submission", () => {
    const html = renderToStaticMarkup(
      <ReviewPanel
        pr={pr}
        demo={false}
        draft={emptyDraft()}
        onDraft={() => {}}
        threads={[]}
        onReloadThreads={() => {}}
        onSubmit={async () => {}}
        onReply={async () => {}}
        onCloseComment={() => {}}
        open={false}
        onCloseReview={() => {}}
      />,
    );
    expect(html).toContain('class="review-summary-dialog"');
    expect(html).toContain('id="review-summary"');
    expect(html).toContain('id="review-decision"');
    expect(html.match(/Submit review to GitHub/g)).toHaveLength(1);
    expect(html).not.toContain("Submit your review to GitHub?");
  });
});
