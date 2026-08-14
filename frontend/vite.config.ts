import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import { blogPlugin } from './scripts/blog-plugin'

export default defineConfig({
  plugins: [blogPlugin(), vue(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@openapi': fileURLToPath(new URL('../openapi', import.meta.url)),
    },
  },
  server: {
    // Dev-serving only: lets the dev server read ../openapi/schema.ts.
    fs: { allow: [fileURLToPath(new URL('..', import.meta.url))] },
  },
  test: {
    // Global env stays 'node' (fast for pure-logic tests). DOM-dependent tests
    // opt in per-file with `// @vitest-environment jsdom`.
    environment: 'node',
    // The plain test gate is unchanged; `npm run test:coverage` enforces a
    // ratchet set ~5 points under actuals — raise these as coverage grows,
    // never lower them.
    coverage: {
      provider: 'v8',
      include: ['src/**', 'scripts/**'],
      reporter: ['text', 'html', 'lcov'],
      thresholds: { lines: 69, statements: 67, branches: 63, functions: 55 },
    },
  },
})
