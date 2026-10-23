import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
  },
  build: {
    // Phaser(약 1.2MB)는 사무실 화면에서만 동적 import로 분리해 받는다.
    chunkSizeWarningLimit: 1600,
  },
});
