import { execSync } from "node:child_process";
import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The dev server proxies every server-owned path to the Go server so component
// code only ever uses relative URLs. HTTP_ADDR and WEB_PORT come from .env.dev
// through scripts/dev.sh.
const serverPort = (process.env.HTTP_ADDR ?? ":8080").split(":").pop() || "8080";
const serverURL = `http://localhost:${serverPort}`;
const webPort = Number(process.env.WEB_PORT ?? 5173);

function git(args: string, fallback: string): string {
  try {
    return execSync(`git ${args}`, { stdio: ["ignore", "pipe", "ignore"] }).toString().trim();
  } catch {
    return fallback;
  }
}

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  define: {
    "import.meta.env.VERSION": JSON.stringify(process.env.VERSION ?? git("describe --tags --always --dirty", "dev")),
    "import.meta.env.COMMIT": JSON.stringify(process.env.COMMIT ?? git("rev-parse --short HEAD", "unknown")),
    "import.meta.env.BUILD_DATE": JSON.stringify(process.env.DATE ?? new Date().toISOString()),
    "import.meta.env.NODE_VERSION": JSON.stringify(process.version),
  },
  server: {
    host: "0.0.0.0",
    port: webPort,
    strictPort: true,
    proxy: {
      "/api": serverURL,
      "/healthz": serverURL,
      "/metrics": serverURL,
      "/about": serverURL,
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/app/test-setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    css: false,
  },
});
