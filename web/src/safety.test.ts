import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./api";
import { demoPR } from "./demo";
import { draftKey } from "./ReviewPanel";
import { pageRows, sourceWindow } from "./window";
import { parsePatch } from "./diff";

afterEach(() => vi.unstubAllGlobals());

describe("review delivery uncertainty", () => {
  it("does not encourage duplicate submission after a connection loss", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockRejectedValue(new TypeError("network lost")),
    );
    await expect(api("review", {})).rejects.toThrow(
      "Outcome unknown. Check GitHub before retrying",
    );
    await expect(api("reply", {})).rejects.toThrow(
      "Outcome unknown. Check GitHub before retrying",
    );
  });
  it("does not treat an invalid success payload as proof of delivery", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response("not JSON", { status: 200 })),
    );
    await expect(api("review", {})).rejects.toThrow("Outcome unknown");
  });
  it("preserves an explicit server rejection", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ error: "The PR base changed. Reload it." }),
            { status: 409 },
          ),
        ),
    );
    await expect(api("review", {})).rejects.toThrow("The PR base changed");
  });
});

it("keeps old-side drafts separate when only the base revision changes", () => {
  expect(draftKey(demoPR, false)).not.toBe(
    draftKey({ ...demoPR, baseSHA: "c".repeat(40) }, false),
  );
  expect(draftKey(demoPR, false)).not.toBe(
    draftKey({ ...demoPR, headSHA: "c".repeat(40) }, false),
  );
  expect(draftKey(demoPR, false)).not.toBe(draftKey(demoPR, true));
});

describe("bounded code rendering", () => {
  it("keeps target lines in a bounded source window at file boundaries", () => {
    for (const target of [0, 199, 200, 4095, 9999]) {
      const window = sourceWindow(10000, target);
      expect(window.end - window.start).toBeLessThanOrEqual(200);
      expect(target).toBeGreaterThanOrEqual(window.start);
      expect(target).toBeLessThan(window.end);
    }
  });
  it("preserves absolute diff line positions after the first page", () => {
    const patch =
      "@@ -1,0 +1,900 @@\n" +
      Array.from({ length: 900 }, (_, i) => `+value${i}`).join("\n");
    const page = pageRows(parsePatch(patch), 1);
    expect(page.rows).toHaveLength(400);
    expect(page.rows[0].newLine).toBe(400);
    expect(page.rows.at(-1)?.newLine).toBe(799);
  });
});
