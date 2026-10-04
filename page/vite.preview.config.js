import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
export default defineConfig({
  plugins: [vue()],
  server: { host: '127.0.0.1', port: 4173, strictPort: true, proxy: { '/api/v1/mos/plugins/query': { target: process.env.MOS_PREVIEW_BACKEND || 'http://127.0.0.1:8766' } } },
  build: { outDir: 'preview-dist', rollupOptions: { input: 'preview/index.html' } },
});
