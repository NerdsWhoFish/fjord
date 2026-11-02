import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { githubImageURL, MarkdownContent } from "./MarkdownContent";
import { MarkdownEditor } from "./MarkdownEditor";
import { codeFencePath, languageFor } from "./syntax-language";

const render = (text: string) =>
  renderToStaticMarkup(<MarkdownContent>{text}</MarkdownContent>);

describe("GitHub-flavored Markdown and safe HTML", () => {
  it("shares code language mappings with the diff syntax highlighter", () => {
    for (const [fence, grammar] of [
      ["javascript", "javascript"],
      ["typescript", "typescript"],
      ["python", "python"],
      ["bash", "bash"],
      ["terraform", "hcl"],
      ["dockerfile", "docker"],
      ["go", "go"],
    ])
      expect(languageFor(codeFencePath(fence)!)).toBe(grammar);
    expect(codeFencePath("unsupported")).toBeUndefined();
  });
  it("preserves comment line breaks like GitHub conversation fields", () => {
    expect(render("First line\nSecond line")).toContain("<br/>");
  });
  it("renders tables, task lists, strikethrough, code, and safe HTML", () => {
    const html = render(
      [
        "| Status | Review |",
        "| --- | --- |",
        "| Ready | **Ship it** |",
        "",
        "- [x] Reviewed",
        "",
        "~~obsolete~~",
        "",
        "```go",
        "var ready = true",
        "```",
        "",
        "<details><summary>Context</summary><p><em>Checked</em><br>Again</p></details>",
      ].join("\n"),
    );
    expect(html).toContain("<table>");
    expect(html).toContain("<strong>Ship it</strong>");
    expect(html).toContain('type="checkbox"');
    expect(html).toContain("disabled");
    expect(html).toContain("<del>obsolete</del>");
    expect(html).toContain('class="language-go"');
    expect(html).toContain("<summary>Context</summary>");
    expect(html).toContain("<em>Checked</em>");
  });

  it("strips executable and layout-changing HTML", () => {
    const html = render(
      [
        '<script>alert("private")</script>',
        "<style>body { display: none }</style>",
        '<iframe src="https://evil.example"></iframe>',
        '<form action="/api/review"><input name="body" value="hidden"></form>',
        '<object data="https://evil.example"></object>',
        '<svg><a xlink:href="javascript:alert(1)">bad</a></svg>',
        '<p id="review-summary" style="position:fixed" onclick="alert(1)">Safe text</p>',
      ].join("\n"),
    );
    for (const forbidden of [
      "<script",
      "<style",
      "<iframe",
      "<form",
      "<object",
      "<svg",
      "onclick=",
      "position:fixed",
      "javascript:",
      'id="review-summary"',
    ])
      expect(html).not.toContain(forbidden);
    expect(html).toContain("Safe text");
  });

  it("rejects dangerous URLs and protects external links", () => {
    const html = render(
      '[bad](javascript:alert%281%29)\n\n<a href="data:text/html,bad">data</a>\n\n[good](https://github.com/NerdsWhoFish/fjord)',
    );
    expect(html).not.toContain("javascript:");
    expect(html).not.toContain("data:text");
    expect(html).toContain('rel="noopener noreferrer"');
    expect(html).toContain('href="https://github.com/NerdsWhoFish/fjord"');
  });

  it("loads GitHub attachment images but turns arbitrary remote images into links", () => {
    const html = render(
      '<img width="867" height="524" src="https://github.com/user-attachments/assets/abc" onerror="alert(1)" alt="Screenshot">\n\n![External](https://tracker.example/pixel.png)',
    );
    expect(html).toContain(
      '<img src="https://github.com/user-attachments/assets/abc"',
    );
    expect(html).toContain('loading="lazy"');
    expect(html).toContain('referrerPolicy="no-referrer"');
    expect(html).not.toContain("onerror");
    expect(html).toContain('href="https://tracker.example/pixel.png"');
    expect(html).not.toContain('<img src="https://tracker.example');
    expect(render("![bad](data:image/svg+xml,test)")).not.toContain("<img");
  });

  it("does not trust lookalike hosts, credentials, ports, or local image paths", () => {
    expect(
      githubImageURL("https://user-images.githubusercontent.com/test.png"),
    ).toBe("https://user-images.githubusercontent.com/test.png");
    for (const url of [
      "http://github.com/user-attachments/test",
      "https://githubusercontent.com.evil.example/x",
      "https://evilgithubusercontent.com/x",
      "https://user:pass@github.com/user-attachments/x", // betterleaks:allow synthetic credentials verify URL rejection.
      "https://github.com:8443/user-attachments/x",
      "https://github.com/elsewhere/x",
      "https://untrusted.s3.amazonaws.com/x",
      "/api/pr",
    ])
      expect(githubImageURL(url)).toBeUndefined();
  });
});

describe("Markdown editor preview", () => {
  it("keeps the textarea and its value mounted while showing sanitized preview", () => {
    const html = renderToStaticMarkup(
      <MarkdownEditor
        label="Comment"
        id="comment"
        value="**Saved draft**"
        readOnly
        preview
      />,
    );
    expect(html).toContain("<textarea");
    expect(html).toContain("**Saved draft**</textarea>");
    expect(html).toContain("<strong>Saved draft</strong>");
    expect(html).toContain('role="tablist"');
    expect(html).toContain('aria-selected="true"');
    expect(html).toContain('hidden=""');
  });
  it("shows an empty preview and keeps the write field disabled while sending", () => {
    const html = renderToStaticMarkup(
      <MarkdownEditor label="Reply" value="" preview disabled />,
    );
    expect(html).toContain("Nothing to preview");
    expect(html).toContain("disabled");
  });
});
