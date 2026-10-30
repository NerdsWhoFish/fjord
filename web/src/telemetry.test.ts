import { afterEach, expect, it, vi } from "vitest";

const initialize = vi.hoisted(() => vi.fn());
vi.mock("@nerdswhofish/browser-telemetry", () => ({
  initializeTelemetry: initialize,
}));
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.resetModules();
  initialize.mockReset();
});

it("uses an absolute same-origin collector URL on loopback", async () => {
  vi.stubGlobal("location", { origin: "http://127.0.0.1:4727" });
  vi.stubGlobal("document", { scripts: [] });
  initialize.mockReturnValue({ captureError: vi.fn() });
  const { startTelemetry } = await import("./telemetry");
  startTelemetry();
  const config = initialize.mock.calls[0][0];
  expect(new URL(config.url).origin).toBe(location.origin);
  expect(config.local).toBe(true);
});

it("keeps startup usable and reports only a fixed envelope if the SDK fails", async () => {
  vi.stubGlobal("location", { origin: "http://127.0.0.1:4727" });
  vi.stubGlobal("document", { scripts: [] });
  const request = vi
    .fn()
    .mockResolvedValue(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", request);
  vi.spyOn(console, "warn").mockImplementation(() => {});
  initialize.mockImplementation(() => {
    throw new Error("private source and token");
  });
  const { startTelemetry } = await import("./telemetry");
  expect(startTelemetry).not.toThrow();
  expect(request.mock.calls[0][0]).toBe("/api/telemetry");
  expect(request.mock.calls[0][1].body).not.toMatch(/private|source|token/);
});
