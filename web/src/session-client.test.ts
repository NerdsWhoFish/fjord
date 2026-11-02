import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SessionHTTPError,
  startReviewSession,
  bindSessionLifecycle,
  sessionTransport,
  type ActionContext,
  type SessionAction,
  type SessionState,
  type SessionTransport,
} from "./session-client";
import { editDraftComment, emptyDraft, type ReviewDraft } from "./ReviewPanel";

const pr = {
  url: "https://github.com/example/repo/pull/1",
  headSHA: "head",
  baseSHA: "base",
};
const action: SessionAction = {
  id: "a1",
  kind: "draft",
  comment: {
    id: "a1",
    path: "main.go",
    line: 1,
    side: "RIGHT",
    body: "Suggested fix",
    source: "ai",
  },
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((accept, decline) => {
    resolve = accept;
    reject = decline;
  });
  return { promise, resolve, reject };
}
function transport(handler: (endpoint: string, body: any) => Promise<any>) {
  return vi.fn(handler) as unknown as SessionTransport &
    ReturnType<typeof vi.fn>;
}
async function flush() {
  await vi.advanceTimersByTimeAsync(0);
}

describe("browser review session synchronization", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
  });

  it("waits for action application before acknowledging and preserves newer human edits", async () => {
    let draft = emptyDraft();
    const applying = deferred<void>();
    const calls: any[] = [];
    const send = transport(async (endpoint, body) => {
      calls.push({ endpoint, body });
      if (endpoint === "open") return { id: "s1" };
      if (endpoint === "close") return {};
      return {
        actions:
          calls.filter((call) => call.endpoint === "sync").length === 1
            ? [action]
            : [],
      };
    });
    const runner = startReviewSession({
      pr,
      snapshot: () => ({ draft }),
      transport: send,
      onState: () => {},
      onAction: async (received) => {
        await applying.promise;
        draft = { ...draft, comments: [...draft.comments, received.comment!] };
      },
    });
    await flush();
    draft = { ...draft, body: "Human summary written during navigation." };
    await vi.advanceTimersByTimeAsync(10_000);
    expect(calls.filter((call) => call.endpoint === "sync")).toHaveLength(1);
    applying.resolve();
    await flush();
    await vi.advanceTimersByTimeAsync(2000);
    const sync = calls.filter((call) => call.endpoint === "sync").at(-1).body;
    expect(sync.draft.body).toBe("Human summary written during navigation.");
    expect(sync.draft.comments).toHaveLength(1);
    expect(sync.acknowledgements).toEqual([{ id: "a1", status: "applied" }]);
    runner.stop();
  });

  it("retries lost acknowledgements and never reapplies duplicate queue entries", async () => {
    let calls = 0;
    const acknowledgements: unknown[] = [];
    const apply = vi.fn(async () => {});
    const send = transport(async (endpoint, body) => {
      if (endpoint === "open") return { id: "s1" };
      if (endpoint === "close") return {};
      calls++;
      acknowledgements.push(body.acknowledgements);
      if (calls === 2) throw new Error("Connection interrupted");
      return { actions: calls <= 3 ? [action, action] : [] };
    });
    const runner = startReviewSession({
      pr,
      snapshot: () => ({ draft: emptyDraft() }),
      transport: send,
      onState: () => {},
      onAction: apply,
    });
    await flush();
    await vi.advanceTimersByTimeAsync(6000);
    expect(apply).toHaveBeenCalledTimes(1);
    expect(acknowledgements[1]).toEqual([{ id: "a1", status: "applied" }]);
    expect(acknowledgements[2]).toEqual(acknowledgements[1]);
    runner.stop();
  });

  it("reconnects only on explicit missing-session responses", async () => {
    let syncs = 0;
    let opens = 0;
    const states: SessionState[] = [];
    const send = transport(async (endpoint) => {
      if (endpoint === "open") return { id: `s${++opens}` };
      if (endpoint === "close") return {};
      syncs++;
      if (syncs === 1) throw new SessionHTTPError("Proxy not found", 404);
      if (syncs === 2)
        throw new SessionHTTPError("Expired", 404, "session_not_found");
      return { actions: [] };
    });
    const runner = startReviewSession({
      pr,
      snapshot: () => ({ draft: emptyDraft() }),
      transport: send,
      onState: (state) => states.push(state),
      onAction: async () => {},
    });
    await flush();
    expect(opens).toBe(1);
    await vi.advanceTimersByTimeAsync(2000);
    expect(opens).toBe(1);
    await vi.advanceTimersByTimeAsync(4000);
    expect(opens).toBe(2);
    expect(states.at(-1)).toMatchObject({ id: "s2", status: "connected" });
    runner.stop();
  });

  it("aborts stale source work and does not acknowledge an action after switching PRs", async () => {
    const applying = deferred<void>();
    let context: ActionContext | undefined;
    let changed = false;
    const send = transport(async (endpoint) =>
      endpoint === "open" ? { id: "s1" } : { actions: [action] },
    );
    const runner = startReviewSession({
      pr,
      snapshot: () => ({ draft: emptyDraft() }),
      transport: send,
      onState: () => {},
      onAction: async (_, current) => {
        context = current;
        await applying.promise;
        if (current.isCurrent()) changed = true;
      },
    });
    await flush();
    runner.stop();
    expect(context!.signal.aborted).toBe(true);
    applying.resolve();
    await flush();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(changed).toBe(false);
    expect(send.mock.calls.filter((call) => call[0] === "sync")).toHaveLength(
      1,
    );
    expect(send.mock.calls.filter((call) => call[0] === "close")).toHaveLength(
      1,
    );
  });

  it("closes an abandoned open session even if the transport ignores abort", async () => {
    const opened = deferred<{ id: string }>();
    const send = transport(async (endpoint) =>
      endpoint === "open" ? opened.promise : {},
    );
    const runner = startReviewSession({
      pr,
      snapshot: () => ({ draft: emptyDraft() }),
      transport: send,
      onState: () => {},
      onAction: async () => {},
    });
    runner.stop();
    opened.resolve({ id: "late" });
    await flush();
    expect(send.mock.calls.map((call) => call[0])).toEqual(["open", "close"]);
    expect(send.mock.calls[1][1]).toEqual({ id: "late" });
  });

  it("acknowledges failed actions without retrying their side effects", async () => {
    const states: SessionState[] = [];
    const apply = vi.fn(async () => {
      throw new Error("Cannot open target");
    });
    const send = transport(async (endpoint) =>
      endpoint === "open" ? { id: "s1" } : { actions: [action] },
    );
    const runner = startReviewSession({
      pr,
      snapshot: () => ({ draft: emptyDraft() }),
      transport: send,
      onState: (state) => states.push(state),
      onAction: apply,
    });
    await flush();
    expect(states.at(-1)?.error).toBe("Cannot open target");
    await vi.advanceTimersByTimeAsync(2000);
    expect(apply).toHaveBeenCalledTimes(1);
    const body = send.mock.calls.filter(
      (call) => call[0] === "sync",
    )[1][1] as any;
    expect(body.acknowledgements[0]).toMatchObject({
      id: "a1",
      status: "failed",
    });
    runner.stop();
  });

  it("closes on pagehide and leaves no polling session after normal navigation", async () => {
    const page = new EventTarget();
    const send = transport(async (endpoint) =>
      endpoint === "open" ? { id: "s1" } : { actions: [] },
    );
    const lifecycle = bindSessionLifecycle(page, () =>
      startReviewSession({
        pr,
        snapshot: () => ({ draft: emptyDraft() }),
        transport: send,
        onState: () => {},
        onAction: async () => {},
      }),
    );
    await flush();
    page.dispatchEvent(new Event("pagehide"));
    await flush();
    expect(send.mock.calls.filter((call) => call[0] === "close")).toHaveLength(
      1,
    );
    page.dispatchEvent(
      Object.assign(new Event("pageshow"), { persisted: false }),
    );
    await vi.advanceTimersByTimeAsync(10_000);
    expect(send.mock.calls.filter((call) => call[0] === "open")).toHaveLength(
      1,
    );
    expect(send.mock.calls.filter((call) => call[0] === "sync")).toHaveLength(
      1,
    );
    lifecycle.stop();
    page.dispatchEvent(
      Object.assign(new Event("pageshow"), { persisted: true }),
    );
    await flush();
    expect(send.mock.calls.filter((call) => call[0] === "open")).toHaveLength(
      1,
    );
  });

  it("reopens exactly once after BFCache restore with the latest draft", async () => {
    const page = new EventTarget();
    let draft = emptyDraft();
    let opens = 0;
    const send = transport(async (endpoint) =>
      endpoint === "open" ? { id: `s${++opens}` } : { actions: [] },
    );
    const lifecycle = bindSessionLifecycle(page, () =>
      startReviewSession({
        pr,
        snapshot: () => ({ draft }),
        transport: send,
        onState: () => {},
        onAction: async () => {},
      }),
    );
    await flush();
    page.dispatchEvent(new Event("visibilitychange"));
    expect(send.mock.calls.filter((call) => call[0] === "close")).toHaveLength(
      0,
    );
    page.dispatchEvent(
      Object.assign(new Event("pagehide"), { persisted: true }),
    );
    draft = { ...draft, body: "Draft retained in the back-forward cache" };
    page.dispatchEvent(
      Object.assign(new Event("pageshow"), { persisted: true }),
    );
    page.dispatchEvent(
      Object.assign(new Event("pageshow"), { persisted: true }),
    );
    await flush();
    expect(opens).toBe(2);
    expect(
      send.mock.calls.filter((call) => call[0] === "open")[1][1],
    ).toMatchObject({ draft });
    lifecycle.stop();
    expect(send.mock.calls.filter((call) => call[0] === "close")).toHaveLength(
      2,
    );
  });

  it("uses a keepalive request to close a departing page session", async () => {
    const fetch = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(Response.json({ closed: true }));
    try {
      await sessionTransport("close", { id: "s1" });
      expect(fetch).toHaveBeenCalledWith(
        "/api/session/close",
        expect.objectContaining({
          method: "POST",
          keepalive: true,
          body: JSON.stringify({ id: "s1" }),
        }),
      );
    } finally {
      fetch.mockRestore();
    }
  });
});

describe("editing assistant review drafts", () => {
  it("preserves attribution and newer comments while editing a draft body", () => {
    const draft: ReviewDraft = {
      ...emptyDraft(),
      body: "Human summary",
      comments: [
        action.comment!,
        {
          ...action.comment!,
          id: "human",
          source: undefined,
          body: "Human comment",
        },
      ],
    };
    const next = editDraftComment(draft, "a1", "  Corrected suggestion  ");
    expect(next.comments[0]).toMatchObject({
      source: "ai",
      body: "Corrected suggestion",
    });
    expect(next.comments[1]).toBe(draft.comments[1]);
    expect(next.body).toBe("Human summary");
    expect(draft.comments[0].body).toBe("Suggested fix");
  });
  it("rejects empty, oversize, and removed draft updates", () => {
    const draft = { ...emptyDraft(), comments: [action.comment!] };
    expect(() => editDraftComment(draft, "a1", " ")).toThrow();
    expect(() => editDraftComment(draft, "a1", "a".repeat(65537))).toThrow();
    expect(() => editDraftComment(draft, "missing", "replacement")).toThrow();
  });
});
