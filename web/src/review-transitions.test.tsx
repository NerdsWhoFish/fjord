import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { PRLoading } from "./PRLoading";
import { reviewRedirect } from "./review-redirect";
import { demoPR } from "./demo";

describe("PR loading screen", () => {
  it("identifies the requested PR without exposing the landing form", () => {
    const html = renderToStaticMarkup(
      <PRLoading url="https://github.com/spacelift-solutions/demo/pull/229" />,
    );
    expect(html).toContain('role="status"');
    expect(html).toContain('aria-busy="true"');
    expect(html).toContain("Opening pull request");
    expect(html).toContain("spacelift-solutions/demo #229");
    expect(html).toContain("Cancel and return home");
    expect(html).not.toContain("<form");
  });
});

describe("successful review destination", () => {
  it("returns the canonical PR, not a server-supplied review or changes link", () => {
    const pr = {
      ...demoPR,
      owner: "spacelift-solutions",
      repo: "demo",
      number: 229,
      url: "https://example.invalid/untrusted",
    };
    expect(reviewRedirect(pr, 4, 4, false)).toBe(
      "https://github.com/spacelift-solutions/demo/pull/229",
    );
  });
  it("does not redirect a different PR or a demo after asynchronous completion", () => {
    expect(reviewRedirect(demoPR, 4, 5, false)).toBeUndefined();
    expect(reviewRedirect(demoPR, 4, 4, true)).toBeUndefined();
  });
});
