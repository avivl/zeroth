import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@zeroth/api": fileURLToPath(new URL("../pkg/api/gen/ts/client.ts", import.meta.url)),
    },
  },
});
