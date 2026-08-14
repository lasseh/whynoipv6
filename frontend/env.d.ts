/// <reference types="vite/client" />

import 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    description?: string
  }
}

declare global {
  // Opt into Vite's strict env typing: an undeclared import.meta.env.X is a
  // compile error instead of `any` — a typo'd or renamed var can't silently
  // bake '' into the bundle again.
  interface ViteTypeOptions {
    strictImportMetaEnv: unknown
  }
  interface ImportMetaEnv {
    readonly VITE_API_URL?: string
  }
}
