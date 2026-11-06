import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";

beforeEach(() => {
  vi.resetModules();
  vi.stubGlobal("__FJORD_CONFIG__", { publicMode: true });
  vi.stubGlobal("location", new URL("https://fjord.example"));
});
afterEach(() => vi.unstubAllGlobals());

it("disables token entry and explains HTTPS on an insecure public origin", async () => {
  vi.stubGlobal("location", new URL("http://fjord.example"));
  const { PublicAccess } = await import("./PublicAccess");
  const html = renderToStaticMarkup(
    <PublicAccess>
      <p>Private pull request</p>
    </PublicAccess>,
  );
  expect(html).not.toContain("Private pull request");
  expect(html).toMatch(/<input[^>]+disabled=""/);
  expect(html).toContain("Open Fjord over HTTPS");
});

it("gates review content behind a password field with storage disclosure", async () => {
  const { PublicAccess } = await import("./PublicAccess");
  const html = renderToStaticMarkup(
    <PublicAccess>
      <p>Private pull request</p>
    </PublicAccess>,
  );
  expect(html).not.toContain("Private pull request");
  expect(html).toContain('type="password"');
  expect(html).toContain("local storage");
  expect(html).toContain("sent to Fjord");
  expect(html).not.toContain("Manage review sessions");
});

it("keeps private deployments ungated", async () => {
  vi.stubGlobal("__FJORD_CONFIG__", { publicMode: false });
  const { PublicAccess } = await import("./PublicAccess");
  expect(
    renderToStaticMarkup(
      <PublicAccess>
        <p>Local review</p>
      </PublicAccess>,
    ),
  ).toBe("<p>Local review</p>");
});

it.each(["502", "network"])(
  "can forget a saved token after startup validation fails with %s",
  async (failure) => {
    const token = "example_token_for_local_tests_only";
    const id = "11111111-1111-1111-1111-111111111111";
    const key = "fjord:github-credential";
    const stored = new Map([[key, JSON.stringify({ token, id })]]);
    const drafts = new Map([[`fjord:public:${id}:review`, "Private draft"]]);
    vi.stubGlobal("localStorage", {
      getItem: (name: string) => stored.get(name) ?? null,
      removeItem: (name: string) => stored.delete(name),
    });
    vi.stubGlobal("sessionStorage", {
      get length() {
        return drafts.size;
      },
      key: (index: number) => [...drafts.keys()][index] ?? null,
      removeItem: (name: string) => drafts.delete(name),
    });
    vi.stubGlobal("window", new EventTarget());
    const request = vi
      .fn()
      .mockImplementation(() =>
        failure === "network"
          ? Promise.reject(new TypeError("Network unavailable"))
          : Promise.resolve(new Response("{}", { status: 502 })),
      );
    vi.stubGlobal("fetch", request);
    const { authSnapshot, initializeAuth, forgetToken } = await import(
      "./auth"
    );
    const { PublicAccess } = await import("./PublicAccess");
    const render = () =>
      renderToStaticMarkup(
        <PublicAccess>
          <p>Private pull request</p>
        </PublicAccess>,
      );
    initializeAuth();
    expect(authSnapshot().status).toBe("checking");
    expect(render()).toContain("Forget token");
    await vi.waitFor(() => expect(authSnapshot().status).toBe("signed-out"));
    const html = render();
    expect(html).toContain("Forget token");
    expect(html).toContain("A GitHub token is saved in this browser.");
    expect(html).not.toContain("Private pull request");
    expect(html).not.toContain(token);
    expect(stored.has(key)).toBe(true);
    expect(drafts.size).toBe(1);
    forgetToken();
    expect(stored.has(key)).toBe(false);
    expect(drafts.size).toBe(0);
    expect(authSnapshot().hasSavedCredential).toBe(false);
    expect(request.mock.calls.map(([url]) => url)).toEqual([
      "/api/auth",
      "/api/auth/logout",
    ]);
    expect(request.mock.calls[1][1].headers.Authorization).toBe(
      `Bearer ${token}`,
    );
    expect(request.mock.calls[1][1].body).toBe("{}");
    expect(render()).not.toContain("Forget token");
    expect(render()).not.toContain("Private pull request");
  },
);

it("documents public MCP authentication without exposing the stored credential", async () => {
  const secret = "example_token_for_local_tests_only";
  vi.stubGlobal("localStorage", { setItem: vi.fn() });
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(
          JSON.stringify({ authenticated: true, login: "reviewer" }),
        ),
      ),
  );
  const { connect } = await import("./auth");
  await connect(secret);
  const { SettingsMenu } = await import("./SettingsMenu");
  const html = renderToStaticMarkup(
    <SettingsMenu session={{ id: "review-session", status: "connected" }} />,
  );
  expect(html).toContain("Manage review sessions");
  expect(html).toContain("Forget token");
  expect(html).toContain("Authorization: Bearer &lt;your GitHub token&gt;");
  expect(html).toContain("only its own");
  expect(html).not.toContain(secret);
});
