/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_LANGUAGE: string
  readonly VITE_API_BASE_URL: string
  // Overrides the browser-storage key prefix derived from the base path
  // (src/lib/storageKey.ts).
  readonly VITE_STORAGE_PREFIX?: string
  // Injected at build time by vite-plugin-package-version (package.json version).
  readonly PACKAGE_VERSION: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
