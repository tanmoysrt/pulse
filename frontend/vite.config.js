import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { viteSingleFile } from 'vite-plugin-singlefile'

// Builds the whole SPA into a single self-contained web/index.html so the Go
// binary can embed one file. sw.js / manifest / logo stay separate static assets.
export default defineConfig({
  plugins: [vue(), viteSingleFile()],
  build: {
    outDir: '../web',
    emptyOutDir: false,
    cssCodeSplit: false,
    assetsInlineLimit: 100000000,
  },
})
