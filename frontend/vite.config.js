import { defineConfig } from 'vite';
import { resolve } from 'node:path';

// Multi-page, not a SPA bundle: each screen is its own document with its own
// entry module, which is what the pages already are. The router comes with the
// native tab bar work; until then MPA keeps the build honest about what ships.
const page = (name) => resolve(__dirname, `${name}.html`);

export default defineConfig({
  // Relative asset URLs so the same build works served from the backend at /
  // and loaded from disk by Tauri's asset protocol.
  base: './',

  build: {
    outDir: 'dist',
    emptyOutDir: true,
    // Tauri targets a known-modern WebView on every platform; there is no old
    // browser to support, so don't ship transpiled fallbacks nobody loads.
    target: ['es2022', 'safari16'],
    rollupOptions: {
      input: {
        index: page('index'),
        connect: page('connect'),
        profiles: page('profiles'),
        settings: page('settings'),
        setup: page('setup'),
      },
    },
  },

  server: {
    port: 5173,
    strictPort: true,
    // `npm run dev` talks to a local server, so /api is proxied rather than
    // needing CORS on the backend.
    proxy: {
      '/api': {
        target: process.env.NORTHROU_API ?? 'http://127.0.0.1:7788',
        changeOrigin: true,
      },
    },
  },
});
