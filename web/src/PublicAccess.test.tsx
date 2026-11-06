import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";

beforeEach(() => {
  vi.resetModules();
  vi.stubGlobal("__FJORD_CONFIG__", { publicMode: true });
});
afterEach(() => vi.unstubAllGlobals());

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
