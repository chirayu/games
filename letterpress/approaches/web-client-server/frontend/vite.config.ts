import { defineConfig } from "vitest/config";
import vue from "@vitejs/plugin-vue";

const apiTarget = process.env.LETTERPRESS_API_TARGET ?? "http://localhost:8080";

export default defineConfig({
  plugins: [vue()],
  server: {
    proxy: {
      "/api": apiTarget,
      "/healthz": apiTarget,
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
  },
});
