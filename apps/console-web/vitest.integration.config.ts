import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  test: {
    name: "integration",
    environment: "jsdom",
    include: ["src/**/*.integration.test.tsx"],
    clearMocks: true,
    restoreMocks: true,
  },
});
