import { describe, expect, it } from "vitest";
import { availableActions } from "./feature-methods";

describe("capability-gated review actions", () => {
  it("offers one direct Go to action for definition, references, or both", () => {
    expect(availableActions(["definition", "references"])).toEqual(["goTo"]);
    expect(availableActions(["references"])).toEqual(["goTo"]);
    expect(availableActions(["definition"])).toEqual(["goTo"]);
  });
  it("keeps implementation and hierarchy tools independently available", () => {
    expect(
      availableActions([
        "implementation",
        "incomingCalls",
        "outgoingCalls",
        "supertypes",
        "subtypes",
      ]),
    ).toEqual([
      "implementation",
      "incomingCalls",
      "outgoingCalls",
      "supertypes",
      "subtypes",
    ]);
  });
  it("limits file tools to supported file-scoped capabilities", () => {
    expect(
      availableActions(
        [
          "definition",
          "rename",
          "diagnostics",
          "documentSymbols",
          "workspaceSymbols",
          "inlayHints",
        ],
        true,
      ),
    ).toEqual(["inlayHints"]);
    expect(availableActions([], true)).toEqual([]);
  });
  it("does not advertise unsupported edits or annotations", () => {
    expect(availableActions(["hover"])).toEqual(["hover"]);
    expect(
      availableActions(["documentHighlights", "codeActions", "rename"]),
    ).toEqual(["documentHighlights", "codeActions", "rename"]);
  });
});
