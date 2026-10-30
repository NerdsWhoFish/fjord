import { defineConfig } from "vite";
import type { ProxyOptions } from "vite";
import react from "@vitejs/plugin-react";
import { execFileSync } from "node:child_process";

const revision =
  process.env.APP_VERSION ||
  execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
if (!/^[a-f0-9]{40}$/.test(revision))
  throw new Error("APP_VERSION must be the full Git commit SHA.");

const backend = "http://127.0.0.1:4727";
const proxy: ProxyOptions = {
  target: backend,
  changeOrigin: true,
  configure(server) {
    server.on("proxyReq", (request, incoming) => {
      // Only our own loopback development UI can cross the API origin boundary.
      if (
        incoming.headers.host === "127.0.0.1:5173" &&
        incoming.headers.origin === "http://127.0.0.1:5173"
      )
        request.setHeader("Origin", backend);
    });
  },
};

export default defineConfig({
  plugins: [react()],
  define: { __BUILD_SHA__: JSON.stringify(revision) },
  server: {
    host: "127.0.0.1",
    port: 5173,
    strictPort: true,
    proxy: { "/api": proxy, "/config.js": proxy },
  },
});
