/** @type {import('eslint').Linter.Config} */
module.exports = {
  root: true,
  extends: [
    'eslint:recommended',
    'plugin:@typescript-eslint/recommended',
    'plugin:svelte/recommended',
    'prettier'
  ],
  parser: '@typescript-eslint/parser',
  plugins: ['@typescript-eslint'],
  parserOptions: {
    sourceType: 'module',
    ecmaVersion: 2022,
    extraFileExtensions: ['.svelte']
  },
  env: {
    browser: true,
    es2022: true,
    node: true
  },
  ignorePatterns: ['node_modules', 'build', '.svelte-kit'],
  overrides: [
    {
      files: ['*.svelte'],
      parser: 'svelte-eslint-parser',
      parserOptions: {
        parser: '@typescript-eslint/parser'
      }
    }
  ],
  globals: {
    // Vite injects this at build time via define() in vite.config.ts;
    // it's intentionally untyped at the source level. Declaring it
    // here so `no-undef` doesn't fire across the components that
    // reference it (e.g. Sidebar.svelte).
    __APP_VERSION__: 'readonly'
  },
  rules: {
    '@typescript-eslint/no-unused-vars': ['warn', { argsIgnorePattern: '^_' }],
    // The Console SPA uses `any` deliberately as the view-model glue
    // type — REST responses arrive untyped, payload maps carry
    // arbitrary JSON, audit events surface unknown nested fields.
    // Forcing a stricter type policy here would mean per-page guards
    // with no operational benefit. Keep as warning so reviewers
    // still notice unintended growth.
    '@typescript-eslint/no-explicit-any': 'warn'
  }
};
