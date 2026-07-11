import js from '@eslint/js'
import tseslint from 'typescript-eslint'
import pluginVue from 'eslint-plugin-vue'
import configPrettier from 'eslint-config-prettier'
import globals from 'globals'

// Flat config for Vue 3 + TypeScript. The blocking gate runs `eslint --quiet`
// (errors only); the full-warning run is `make frontend-check`.
export default tseslint.config(
  {
    ignores: ['dist/**', 'node_modules/**', 'coverage/**'],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  ...pluginVue.configs['flat/recommended'],
  {
    files: ['**/*.{ts,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser },
      // Parse <script lang="ts"> blocks in .vue files with the TS parser.
      parserOptions: { parser: tseslint.parser },
    },
    rules: {
      // TypeScript already resolves identifiers and types; core no-undef only
      // produces false positives on TS sources.
      'no-undef': 'off',
    },
  },
  {
    // Build/tooling files run in Node.
    files: ['*.{js,ts}', 'vite.config.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    // The site's established SFC names are single-word (Home, Header, Tracker, …).
    files: ['src/pages/*.vue', 'src/partials/**/*.vue', 'src/components/*.vue'],
    rules: { 'vue/multi-word-component-names': 'off' },
  },
  configPrettier,
)
