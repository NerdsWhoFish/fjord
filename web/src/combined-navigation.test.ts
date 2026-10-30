import { describe, expect, it, vi } from "vitest";
import { combinedNavigation } from "./combined-navigation";

describe("combined symbol navigation", () => {
  it("groups definitions before references without losing either result", async () => {
    const query = vi.fn(async (method: string) => ({
      locations: [
        { path: method, line: 0, character: 0, endLine: 0, endCharacter: 1 },
      ],
      hover: "",
    }));
    const result = await combinedNavigation(
      ["definition", "references"],
      query,
    );
    expect(query.mock.calls.map(([method]) => method)).toEqual([
      "definition",
      "references",
    ]);
    expect(result.definitions?.[0].path).toBe("definition");
    expect(result.references?.[0].path).toBe("references");
  });
  it("retains successful definitions and explains a reference failure", async () => {
    const result = await combinedNavigation(
      ["definition", "references"],
      async (method) => {
        if (method === "references") throw new Error("Index unavailable");
        return { locations: [], hover: "" };
      },
    );
    expect(result.message).toContain("References: Index unavailable");
  });
  it("does not call methods absent from server capabilities", async () => {
    const query = vi.fn(async () => ({ locations: [], hover: "" }));
    const result = await combinedNavigation(["references"], query);
    expect(query).toHaveBeenCalledExactlyOnceWith("references");
    expect(result.message).toContain("Definitions are not supported");
    await expect(combinedNavigation([], query)).rejects.toThrow(
      "does not support",
    );
  });
});
