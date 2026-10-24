import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  // Relative asset URLs so Dropcatch works under any --base-path.
  base: './',
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022' },
  server: {
    proxy: { '/api': process.env.DROPCATCH_API ?? 'http://127.0.0.1:8080' },
  },
})
