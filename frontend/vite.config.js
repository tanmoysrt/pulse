import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { viteSingleFile } from 'vite-plugin-singlefile'

// Builds the SPA into a single self-contained dist/index.html the Go binary
// embeds. Files in public/ (sw.js, manifest) are copied to dist/ verbatim.
export default defineConfig({
  plugins: [vue(), viteSingleFile()],
  build: {
    outDir: 'dist',
    cssCodeSplit: false,
    assetsInlineLimit: 100000000,
  },
})
