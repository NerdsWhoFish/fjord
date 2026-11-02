import { describe, expect, it, vi } from "vitest";
import { watchCapabilities } from "./navigation-capabilities";
import type { CapabilityState } from "./navigation-capabilities";
import type { NavigationCapabilities, Selection } from "./types";

const selection: Selection = {
  path: "worker.py",
  side: "RIGHT",
  line: 3,
  sha: "head-sha",
  character: 2,
  word: "handler",
  code: "handler()",
};

const capabilities: NavigationCapabilities = {
  server: "custom-python",
  languageId: "python",
  methods: ["definition", "hover"],
};

function deferred() {
  let resolve!: (value: NavigationCapabilities) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<NavigationCapabilities>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

describe("contextual navigation capabilities", () => {
  it("loads actual server capabilities for a configured language", async () => {
    const request = deferred();
    const load = vi.fn(() => request.promise);
    const report = vi.fn();
    watchCapabilities(selection, load, report);
    expect(load).toHaveBeenCalledWith(selection);
    expect(report.mock.calls).toEqual([[{ status: "loading" }]]);
    request.resolve(capabilities);
    await request.promise;
    expect(report).toHaveBeenLastCalledWith({ status: "ready", capabilities });
  });

  it("never starts capability requests for line or range menus", () => {
    const load = vi.fn();
    const report = vi.fn();
    watchCapabilities({ ...selection, word: "", startLine: 1 }, load, report)();
    expect(load).not.toHaveBeenCalled();
    expect(report).not.toHaveBeenCalled();
  });

  it("ignores an older anchor resolving after the current anchor", async () => {
    const old = deferred();
    const current = deferred();
    const states: CapabilityState[] = [];
    const stop = watchCapabilities(
      selection,
      () => old.promise,
      (state) => states.push(state),
    );
    stop();
    watchCapabilities(
      { ...selection, path: "other.py" },
      () => current.promise,
      (state) => states.push(state),
    );
    current.resolve(capabilities);
    await current.promise;
    old.resolve({ ...capabilities, methods: ["implementation"] });
    await old.promise;
    expect(states).toEqual([
      { status: "loading" },
      { status: "loading" },
      { status: "ready", capabilities },
    ]);
  });

  it("ignores failures after the popup closes", async () => {
    const request = deferred();
    const report = vi.fn();
    const stop = watchCapabilities(selection, () => request.promise, report);
    stop();
    request.reject(new Error("unavailable"));
    await request.promise.catch(() => {});
    expect(report.mock.calls).toEqual([[{ status: "loading" }]]);
  });

  it("reports a local failure and permits a successful retry", async () => {
    const request = deferred();
    const report = vi.fn();
    const stop = watchCapabilities(selection, () => request.promise, report);
    request.reject(new Error("private diagnostic"));
    await request.promise.catch(() => {});
    expect(report).toHaveBeenLastCalledWith({ status: "error" });
    stop();
    watchCapabilities(selection, async () => capabilities, report);
    await Promise.resolve();
    expect(report).toHaveBeenLastCalledWith({ status: "ready", capabilities });
  });

  it("handles a loader throwing before returning a promise", () => {
    const report = vi.fn();
    watchCapabilities(
      selection,
      () => {
        throw new Error("unavailable");
      },
      report,
    );
    expect(report).toHaveBeenLastCalledWith({ status: "error" });
  });
});
