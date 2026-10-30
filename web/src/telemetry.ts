import { initializeTelemetry } from "@nerdswhofish/browser-telemetry";
import type { Config } from "./types";

declare global {
  var __FJORD_CONFIG__: Config | undefined;
}

declare const __BUILD_SHA__: string;
let telemetry: ReturnType<typeof initializeTelemetry> | undefined;
export function startTelemetry() {
  if (telemetry) return;
  telemetry = initializeTelemetry({
    url: "/api/telemetry",
    local: true,
    app: { name: "fjord", version: __BUILD_SHA__, environment: "local" },
    routes: [
      "/",
      "/api/pr",
      "/api/config",
      "/api/content",
      "/api/workspace",
      "/api/workspace/file",
      "/api/navigate",
      "/api/review",
      "/api/comments",
      "/api/reply",
      "/api/assist",
      "/api/telemetry",
    ],
    assets: Array.from(document.scripts)
      .map((script) => new URL(script.src || location.href).pathname)
      .filter((path) => path.startsWith("/assets/")),
    operations: [
      "pr.load",
      "navigate",
      "review.submit",
      "assist",
      "reply",
      "app.render",
    ],
  });
}
export function captureError(error: unknown, operation: string) {
  telemetry?.captureError(error, operation);
}
