import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const DEV_PORT = 5173;
const DAEMON = 'http://127.0.0.1:9120';
/** Origins of this dev server itself; the daemon only accepts its own origin. */
const DEV_ORIGINS = new Set([`http://127.0.0.1:${DEV_PORT}`, `http://localhost:${DEV_PORT}`]);

function rewriteSameOrigin(proxy: { on: (event: 'proxyReq', cb: (proxyReq: { setHeader: (k: string, v: string) => void }, req: { headers: { origin?: string } }) => void) => void }) {
  proxy.on('proxyReq', (proxyReq, req) => {
    const origin = req.headers.origin;
    if (origin && DEV_ORIGINS.has(origin)) proxyReq.setHeader('Origin', DAEMON);
  });
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: DEV_PORT,
    proxy: {
      '/api': {
        target: DAEMON,
        changeOrigin: true,
        configure: rewriteSameOrigin,
      },
      '/mcp': {
        target: DAEMON,
        changeOrigin: true,
        configure: rewriteSameOrigin,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
