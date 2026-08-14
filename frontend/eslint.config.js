import js from '@eslint/js'
import pluginVue from 'eslint-plugin-vue'
import { defineConfigWithVueTs, vueTsConfigs } from '@vue/eslint-config-typescript'
import configPrettier from 'eslint-config-prettier'
import globals from 'globals'

// Flat config for Vue 3 + TypeScript. defineConfigWithVueTs wires vue-tsc's
// type information into typescript-eslint, so the type-aware tier
// (no-floating-promises &co) sees real .vue module types. The blocking gate
// runs `eslint --max-warnings 0`, so warnings are enforcing too.
export default defineConfigWithVueTs(
  {
    ignores: ['dist/**', 'node_modules/**', 'coverage/**'],
  },
  js.configs.recommended,
  pluginVue.configs['flat/recommended'],
  vueTsConfigs.recommendedTypeChecked,
  {
    files: ['**/*.{ts,vue}'],
    languageOptions: {
      ecmaVersion: 'latest',
      sourceType: 'module',
      globals: { ...globals.browser },
    },
    rules: {
      // TypeScript already resolves identifiers and types; core no-undef only
      // produces false positives on TS sources.
      'no-undef': 'off',
      // Misfires on type-based withDefaults props where `undefined` is the
      // meaningful state and exactOptionalPropertyTypes forbids a default.
      'vue/require-default-prop': 'off',
    },
  },
  {
    // Build/tooling files run in Node.
    files: ['*.{js,ts}', 'vite.config.ts', 'scripts/**/*.ts'],
    languageOptions: { globals: { ...globals.node } },
  },
  {
    // The site's established SFC names are single-word (Home, Header, Tracker, …).
    files: ['src/pages/*.vue', 'src/partials/**/*.vue', 'src/components/**/*.vue'],
    rules: { 'vue/multi-word-component-names': 'off' },
  },
  configPrettier,
)
