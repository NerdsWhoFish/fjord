import { describe, expect, it } from "vitest";
import { insertSuggestion, suggestionUnavailable } from "./suggestion";
import type { Selection } from "./types";

describe("GitHub suggestion insertion", () => {
  it("prefills multiline code and selects only its editable replacement", () => {
    const code = "  first()\n  second()";
    const result = insertSuggestion("", code, 0, 0);
    expect(result.body).toBe("```suggestion\n  first()\n  second()\n```");
    expect(result.body.slice(result.codeStart, result.codeEnd)).toBe(code);
  });

  it("replaces the current selection while retaining surrounding prose", () => {
    const result = insertSuggestion(
      "Before old text after",
      "newCode()",
      7,
      15,
    );
    expect(result.body).toBe(
      "Before \n\n```suggestion\nnewCode()\n```\n\n after",
    );
    expect(result.body.slice(result.codeStart, result.codeEnd)).toBe(
      "newCode()",
    );
  });

  it("inserts at a caret without removing either side", () => {
    const result = insertSuggestion("beforeafter", "next()", 6, 6);
    expect(result.body).toBe("before\n\n```suggestion\nnext()\n```\n\nafter");
  });

  it("keeps existing blank-line separators without adding extra ones", () => {
    const result = insertSuggestion("Before\n\nAfter", "next()", 8, 8);
    expect(result.body).toBe("Before\n\n```suggestion\nnext()\n```\n\nAfter");
  });

  it("uses a fence longer than every backtick run in the selected code", () => {
    const code = "````markdown\n```js\nconst value = `ok`;\n```\n````";
    const result = insertSuggestion("", code, 0, 0);
    expect(result.body).toBe(`\`\`\`\`\`suggestion\n${code}\n\`\`\`\`\``);
    expect(result.body.slice(result.codeStart, result.codeEnd)).toBe(code);
  });

  it("preserves existing newlines and UTF-16 selection offsets", () => {
    const code = "const fish = '\u{1f41f}';\n";
    const result = insertSuggestion("\u{1f41f}", code, 2, 2);
    expect(result.body).toBe(`\u{1f41f}\n\n\`\`\`suggestion\n${code}\n\`\`\``);
    expect(result.body.slice(result.codeStart, result.codeEnd)).toBe(code);
  });

  it("leaves an editable empty replacement for a blank line", () => {
    const result = insertSuggestion("", "", 0, 0);
    expect(result.body).toBe("```suggestion\n\n```");
    expect(result.codeStart).toBe(result.codeEnd);
  });

  it("preserves a selected final blank line before the closing fence", () => {
    const result = insertSuggestion("", "first\n", 0, 0);
    expect(result.body).toBe("```suggestion\nfirst\n\n```");
    expect(result.body.slice(result.codeStart, result.codeEnd)).toBe("first\n");
  });
});

describe("suggestion availability", () => {
  const selection: Selection = {
    path: "main.go",
    side: "RIGHT",
    line: 3,
    startLine: 2,
    sha: "head-sha",
    character: 0,
    word: "",
    code: "first()\nsecond()",
  };

  it("allows complete head ranges and rejects base ranges", () => {
    expect(suggestionUnavailable(selection)).toBeUndefined();
    expect(suggestionUnavailable({ ...selection, side: "LEFT" })).toMatch(
      /head revision/,
    );
  });

  it("does not turn a truncated preview into an incomplete replacement", () => {
    expect(
      suggestionUnavailable({
        ...selection,
        code: "first()\n[Preview limited to 32,768 characters. The comment still covers the selected lines.]",
      }),
    ).toMatch(/truncated/);
  });
});
