import { initializeTelemetry } from "@nerdswhofish/browser-telemetry";
import type { Config } from "./types";

declare global {
  var __FJORD_CONFIG__: Config | undefined;
}

declare const __BUILD_SHA__: string;
let telemetry: ReturnType<typeof initializeTelemetry> | undefined;
export function startTelemetry() {
  if (telemetry) return;
  try {
    telemetry = initializeTelemetry({
      url: new URL("/api/telemetry", location.origin).href,
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
        "/api/navigation/capabilities",
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
        "syntax.highlight",
      ],
    });
  } catch {
    // Instrumentation must not prevent the reviewer from opening their work.
    console.warn("Fjord browser telemetry could not initialize");
    void fetch("/api/telemetry", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        exceptions: [
          { type: "Error", value: "Telemetry initialization failed" },
        ],
      }),
    }).catch(() => {});
  }
}
export function captureError(error: unknown, operation: string) {
  telemetry?.captureError(error, operation);
}
