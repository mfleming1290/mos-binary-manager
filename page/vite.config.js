import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import federation from '@originjs/vite-plugin-federation';
import { mkdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import config from './plugin.config.js';

const version = process.env.PLUGIN_VERSION || config.version;
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
  throw new Error('PLUGIN_VERSION must be numeric x.y.z');
}
const outDir = fileURLToPath(new URL(`./dist/${config.name}/`, import.meta.url));
export default defineConfig({
  base: './',
  plugins: [
    vue(),
    federation({ name: config.name, filename: 'remoteEntry.js', exposes: { './Plugin': './src/Plugin.vue' }, shared: ['vue'] }),
    { name: 'mos-manifest', closeBundle() {
      mkdirSync(outDir, { recursive: true });
      writeFileSync(`${outDir}/manifest.json`, JSON.stringify({ ...config, version }, null, 2) + '\n');
    } },
  ],
  build: { target: 'esnext', minify: false, cssCodeSplit: false, outDir, assetsDir: '', rollupOptions: { input: {} } },
});
