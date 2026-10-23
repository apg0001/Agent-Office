import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // 개발 서버에서 API·WebSocket을 같은 출처로 보이게 한다(쿠키·Origin 검사 일치).
    proxy: {
      "/api": {
        target: process.env.AGENT_OFFICE_API_URL ?? "http://127.0.0.1:8000",
        ws: true,
      },
    },
  },
  build: {
    // Phaser(약 1.2MB)는 사무실 화면에서만 동적 import로 분리해 받는다.
    chunkSizeWarningLimit: 1600,
  },
});
