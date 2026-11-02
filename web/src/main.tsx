import React from "react";
import ReactDOM from "react-dom/client";
import "@fontsource/ibm-plex-sans/400.css";
import "@fontsource/ibm-plex-sans/500.css";
import "@fontsource/ibm-plex-sans/600.css";
import "@fontsource/ibm-plex-mono/400.css";
import "./style.css";
import "./theme.css";
import { App } from "./App";
import { initializeTheme } from "./ThemeToggle";
import { captureError, startTelemetry } from "./telemetry";

initializeTheme();
if (globalThis.__FJORD_CONFIG__?.telemetryEnabled) startTelemetry();
ReactDOM.createRoot(document.getElementById("root")!, {
  onUncaughtError: (error) => captureError(error, "app.render"),
  onCaughtError: (error) => captureError(error, "app.render"),
  onRecoverableError: (error) => captureError(error, "app.render"),
}).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);
