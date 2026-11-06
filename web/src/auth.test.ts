import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const token = "example_token_for_local_tests_only";
const key = "fjord:github-credential";
let local: Map<string, string>;
let drafts: Map<string, string>;
let browser: EventTarget;
let request: ReturnType<typeof vi.fn>;
function storage(values: Map<string, string>) {
  return {
    get length() {
      return values.size;
    },
    key: (index: number) => [...values.keys()][index] ?? null,
    getItem: (name: string) => values.get(name) ?? null,
    setItem: (name: string, value: string) => values.set(name, value),
    removeItem: (name: string) => values.delete(name),
  };
}
beforeEach(() => {
  vi.resetModules();
  local = new Map();
  drafts = new Map();
  browser = new EventTarget();
  request = vi
    .fn()
    .mockImplementation(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({ authenticated: true, login: "reviewer" }),
        ),
      ),
    );
  vi.stubGlobal("fetch", request);
  vi.stubGlobal("localStorage", storage(local));
  vi.stubGlobal("sessionStorage", storage(drafts));
  vi.stubGlobal("window", browser);
  vi.stubGlobal("__FJORD_CONFIG__", { publicMode: true });
});
afterEach(() => vi.unstubAllGlobals());

describe("browser GitHub credentials", () => {
  it("validates before persisting or enabling API access", async () => {
    const auth = await import("./auth");
    await expect(auth.authenticatedFetch("/api/pr", {})).rejects.toThrow(
      "Connect",
    );
    expect(request).not.toHaveBeenCalled();
    let resolve!: (response: Response) => void;
    request.mockImplementationOnce(
      () =>
        new Promise<Response>((done) => {
          resolve = done;
        }),
    );
    const connecting = auth.connect(token);
    expect(auth.authSnapshot().status).toBe("checking");
    expect(local.has(key)).toBe(false);
    resolve(
      new Response(JSON.stringify({ authenticated: true, login: "reviewer" })),
    );
    await connecting;
    expect(auth.authSnapshot()).toMatchObject({
      status: "ready",
      login: "reviewer",
    });
    expect(JSON.parse(local.get(key)!)).toMatchObject({ token });
    expect(auth.credentialID()).not.toContain(token);
    expect(request.mock.calls[0][1].headers.Authorization).toBe(
      `Bearer ${token}`,
    );
    expect(request.mock.calls[0][1].body).toBe("{}");
  });
  it("never persists rejected tokens or includes arbitrary upstream text in errors", async () => {
    request.mockResolvedValue(
      new Response(JSON.stringify({ error: token }), { status: 401 }),
    );
    const auth = await import("./auth");
    await auth.connect(token);
    expect(auth.authSnapshot().status).toBe("signed-out");
    expect(auth.authSnapshot().error).not.toContain(token);
    expect(local.size).toBe(0);
  });
  it("fails clearly when persistent storage is unavailable", async () => {
    vi.stubGlobal("localStorage", {
      setItem: () => {
        throw new Error("blocked");
      },
    });
    const auth = await import("./auth");
    await auth.connect(token);
    expect(auth.authSnapshot()).toMatchObject({ status: "signed-out" });
    expect(auth.authSnapshot().error).toContain("storage is blocked");
  });
  it("does not forward browser tokens in private mode", async () => {
    vi.stubGlobal("__FJORD_CONFIG__", { publicMode: false });
    const auth = await import("./auth");
    const options = {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    };
    await auth.authenticatedFetch("/api/pr", options);
    expect(request).toHaveBeenCalledWith("/api/pr", options);
    auth.initializeAuth();
    expect(request).toHaveBeenCalledTimes(1);
  });
  it("adds auth only to authenticated APIs, without cookies or redirects", async () => {
    const auth = await import("./auth");
    await auth.connect(token);
    await auth.authenticatedFetch("/api/pr", { method: "POST" });
    const options = request.mock.calls[1][1];
    expect(options.headers.get("Authorization")).toBe(`Bearer ${token}`);
    expect(options.credentials).toBe("omit");
    expect(options.redirect).toBe("error");
  });
  it("clears drafts and aborts old requests before forgetting a token", async () => {
    const auth = await import("./auth");
    await auth.connect(token);
    drafts.set("fjord:public:old:private", "private review");
    drafts.set("unrelated", "keep");
    let resolve!: (response: Response) => void;
    request.mockImplementationOnce(
      () =>
        new Promise<Response>((done) => {
          resolve = done;
        }),
    );
    const pending = auth.authenticatedFetch("/api/pr", {});
    const pendingSignal = request.mock.calls[1][1].signal;
    auth.forgetToken();
    expect(pendingSignal.aborted).toBe(true);
    expect(auth.authSnapshot().status).toBe("signed-out");
    expect(local.has(key)).toBe(false);
    expect([...drafts]).toEqual([["unrelated", "keep"]]);
    resolve(new Response("{}"));
    await expect(pending).rejects.toMatchObject({ name: "AbortError" });
    expect(request.mock.calls[2][0]).toBe("/api/auth/logout");
  });
  it("invalidates all private UI after a revoked token returns 401", async () => {
    const auth = await import("./auth");
    await auth.connect(token);
    request.mockResolvedValueOnce(new Response("{}", { status: 401 }));
    await expect(auth.authenticatedFetch("/api/pr", {})).rejects.toThrow(
      "Reconnect",
    );
    expect(auth.authSnapshot().status).toBe("signed-out");
    expect(local.has(key)).toBe(false);
  });
  it("revalidates a saved credential before opening the application", async () => {
    const auth = await import("./auth");
    await auth.connect(token);
    const draftKey = `fjord:public:${auth.credentialID()}:review`;
    drafts.set(draftKey, "draft preserved on refresh");
    vi.resetModules();
    const restored = await import("./auth");
    restored.initializeAuth();
    expect(restored.authSnapshot().status).toBe("checking");
    await vi.waitFor(() =>
      expect(restored.authSnapshot().status).toBe("ready"),
    );
    expect(drafts.get(draftKey)).toBe("draft preserved on refresh");
  });
  it("responds to logout in another tab immediately", async () => {
    const auth = await import("./auth");
    auth.initializeAuth();
    await auth.connect(token);
    local.delete(key);
    const event = Object.assign(new Event("storage"), { key });
    browser.dispatchEvent(event);
    expect(auth.authSnapshot().status).toBe("signed-out");
  });
  it("ignores a late validation result after cancellation", async () => {
    const auth = await import("./auth");
    let resolve!: (response: Response) => void;
    request.mockImplementationOnce(
      () =>
        new Promise<Response>((done) => {
          resolve = done;
        }),
    );
    const pending = auth.connect(token);
    auth.forgetToken();
    resolve(
      new Response(JSON.stringify({ authenticated: true, login: "reviewer" })),
    );
    await pending;
    expect(auth.authSnapshot().status).toBe("signed-out");
    expect(local.has(key)).toBe(false);
  });
  it("does not restore a token when another tab logs out during validation", async () => {
    const auth = await import("./auth");
    auth.initializeAuth();
    let resolve!: (response: Response) => void;
    request.mockImplementationOnce(
      () =>
        new Promise<Response>((done) => {
          resolve = done;
        }),
    );
    const pending = auth.connect(token);
    browser.dispatchEvent(Object.assign(new Event("storage"), { key }));
    resolve(
      new Response(JSON.stringify({ authenticated: true, login: "reviewer" })),
    );
    await pending;
    expect(auth.authSnapshot().status).toBe("signed-out");
    expect(local.has(key)).toBe(false);
  });
  it("rejects malformed credentials without issuing a request", async () => {
    const auth = await import("./auth");
    await auth.connect("not a token\r\ninjected: header");
    expect(request).not.toHaveBeenCalled();
    expect(auth.authSnapshot().error).toContain("valid GitHub");
  });
});
