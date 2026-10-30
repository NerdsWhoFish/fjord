import type { NavigationCapabilities, Selection } from "./types";

export type CapabilityState =
  | { status: "loading" }
  | { status: "ready"; capabilities: NavigationCapabilities }
  | { status: "error" };

export type CapabilityLoader = (
  selection: Selection,
) => Promise<NavigationCapabilities>;

export function watchCapabilities(
  selection: Selection,
  load: CapabilityLoader,
  report: (state: CapabilityState) => void,
): () => void {
  let active = true;
  if (!selection.word) return () => {};
  report({ status: "loading" });
  async function resolve() {
    try {
      const capabilities = await load(selection);
      if (active) report({ status: "ready", capabilities });
    } catch {
      if (active) report({ status: "error" });
    }
  }
  void resolve();
  // A shared cached request can outlive the popup that first requested it.
  return () => {
    active = false;
  };
}
