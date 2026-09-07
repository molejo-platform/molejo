import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";

const tlsCertificate = process.env.VITE_TLS_CERT;
const tlsKey = process.env.VITE_TLS_KEY;

export default defineConfig({
  plugins: [react()],
  server: {
    https: tlsCertificate && tlsKey ? { cert: readFileSync(tlsCertificate), key: readFileSync(tlsKey) } : undefined,
    proxy: {
      "/api": {
        target: process.env.VITE_API_PROXY_TARGET ?? "http://127.0.0.1:8080",
        changeOrigin: false,
      },
    },
  },
  build: {
    sourcemap: true,
  },
});
