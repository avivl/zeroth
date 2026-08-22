import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const daemon = "http://127.0.0.1:8420";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@zeroth/api": fileURLToPath(new URL("../pkg/api/gen/ts/client.ts", import.meta.url)),
    },
  },
  server: {
    proxy: {
      "/health": daemon,
      "/runs": { target: daemon, ws: true },
      "/plans": daemon,
      "/agents": daemon,
      "/approvals": daemon,
      "/memory": daemon,
      "/audit": daemon,
      "/checkpoints": daemon,
    },
  },
});
