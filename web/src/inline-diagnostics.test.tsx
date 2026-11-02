import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import {
  DiagnosticCache,
  diagnosticKey,
  diagnosticsForLine,
} from "./inline-diagnostics";
import { DiagnosticMarker, DiagnosticSummary } from "./InlineDiagnostics";
import { withDiagnostics, annotationParts } from "./code-annotations";
import { AnnotatedText } from "./AnnotatedText";
import type {
  NavigationCapabilities,
  NavigationResult,
  ReviewItem,
  Selection,
} from "./types";

const selected: Selection = {
  path: "main.go",
  sha: "head",
  side: "RIGHT",
  line: 1,
  character: 0,
  word: "main",
  code: "",
};
const supported: NavigationCapabilities = {
  server: "test",
  languageId: "go",
  methods: ["diagnostics"],
};
const report: NavigationResult = { locations: [], hover: "", items: [] };
const item: ReviewItem = {
  label: "Unknown <symbol>",
  kind: "error",
  location: {
    path: "main.go",
    line: 0,
    character: 1,
    endLine: 2,
    endCharacter: 0,
  },
};

describe("automatic diagnostics", () => {
  it("shares requests by revision and path, including mounted source views", async () => {
    const query = vi.fn(async () => report);
    const cache = new DiagnosticCache(async () => supported, query);
    await Promise.all([
      cache.load(selected),
      cache.load({ ...selected, line: 20 }),
    ]);
    expect(query).toHaveBeenCalledTimes(1);
    await cache.load({ ...selected, sha: "base", side: "LEFT" });
    await cache.load({ ...selected, path: "other.go" });
    expect(query).toHaveBeenCalledTimes(3);
    expect(diagnosticKey(selected)).not.toBe(
      diagnosticKey({ ...selected, sha: "base" }),
    );
  });

  it("never queries an unsupported language server", async () => {
    const query = vi.fn(async () => report);
    const cache = new DiagnosticCache(
      async () => ({ ...supported, methods: [] }),
      query,
    );
    expect(await cache.load(selected)).toMatchObject({ status: "unsupported" });
    expect(query).not.toHaveBeenCalled();
  });

  it("distinguishes silent push servers from a clean report and supports explicit retry", async () => {
    const query = vi
      .fn()
      .mockResolvedValueOnce({
        ...report,
        message:
          "The language server has not published diagnostics for this file yet.",
      })
      .mockResolvedValueOnce(report);
    const cache = new DiagnosticCache(async () => supported, query);
    expect(await cache.load(selected)).toMatchObject({
      status: "ready",
      pending: true,
    });
    cache.invalidate(selected);
    expect(await cache.load(selected)).toMatchObject({
      status: "ready",
      pending: false,
    });
    expect(query).toHaveBeenCalledTimes(2);
  });

  it("bounds concurrent requests and releases queued requests after failure", async () => {
    const resolvers: (() => void)[] = [];
    let active = 0;
    let max = 0;
    const cache = new DiagnosticCache(
      async () => supported,
      async () => {
        max = Math.max(max, ++active);
        await new Promise<void>((resolve) => resolvers.push(resolve));
        active--;
        throw new Error("offline");
      },
    );
    const promises = [0, 1, 2, 3].map((index) =>
      cache.load({ ...selected, path: `${index}.go` }),
    );
    await vi.waitFor(() => expect(resolvers.length).toBe(2));
    resolvers.splice(0).forEach((resolve) => resolve());
    await vi.waitFor(() => expect(resolvers.length).toBe(2));
    resolvers.splice(0).forEach((resolve) => resolve());
    expect(await Promise.all(promises)).toEqual(
      Array(4).fill({ status: "error", message: "offline" }),
    );
    expect(max).toBe(2);
  });

  it("isolates file ranges and does not mark an excluded multiline endpoint", () => {
    expect(diagnosticsForLine([item], "main.go", 0)).toEqual([item]);
    expect(diagnosticsForLine([item], "main.go", 1)).toEqual([item]);
    expect(diagnosticsForLine([item], "main.go", 2)).toEqual([]);
    expect(diagnosticsForLine([item], "other.go", 0)).toEqual([]);
  });

  it("keeps syntax, occurrence highlights, diagnostic squiggles and UTF-16 offsets together", () => {
    const code = "a\u{1f41f}b";
    const diagnostic = {
      ...item,
      location: {
        ...item.location!,
        line: 0,
        character: 2,
        endLine: 0,
        endCharacter: 3,
      },
    };
    const annotations = withDiagnostics(
      { highlights: [{ start: 1, end: 3, kind: "read" }], hints: [] },
      [diagnostic],
      0,
      code,
    );
    const parts = annotationParts(code, 0, annotations);
    expect(
      parts.filter((part) => part.kind === "text").map((part) => part.text),
    ).toEqual(["a", "\u{1f41f}", "b"]);
    const html = renderToStaticMarkup(
      <AnnotatedText
        text={code}
        annotations={annotations}
        spans={[{ start: 0, end: 4, kind: "keyword" }]}
      />,
    );
    expect(html).toContain("code-diagnostic diagnostic-error");
    expect(html).toContain("code-document-highlight highlight-read");
    expect(html).toContain("syntax-keyword");
    expect(html).toContain("Unknown &lt;symbol&gt;");
    expect(html).not.toContain("<symbol>");
  });

  it("labels revision and severity accessibly without hiding comment line buttons", () => {
    const html = renderToStaticMarkup(
      <DiagnosticMarker items={[item]} revision="Base" onOpen={() => {}} />,
    );
    expect(html).toContain("Base error:");
    expect(html).toContain('aria-haspopup="dialog"');
    const pending = renderToStaticMarkup(
      <DiagnosticSummary
        state={{
          status: "ready",
          items: [],
          message: "not published",
          pending: true,
        }}
        visible
        revision="Head"
        onToggle={() => {}}
        onRetry={() => {}}
      />,
    );
    expect(pending).toContain("Diagnostics pending");
    expect(pending).toContain("Retry diagnostics");
    expect(pending).not.toContain("0 errors");
  });
});
