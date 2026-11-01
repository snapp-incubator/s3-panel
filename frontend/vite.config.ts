import path from 'node:path'
import react from '@vitejs/plugin-react'
import { defineConfig, type ProxyOptions } from 'vite'
import version from 'vite-plugin-package-version'

import packageJson from './package.json'

const PROJECT_NAME = 'snappCost'

const DEV_BACKEND = process.env.VITE_DEV_BACKEND || 'http://localhost:8090'

const VITE_APP_ENVIRONMENT = process.env.VITE_APP_ENVIRONMENT

const IS_PRODUCTION_ENV = VITE_APP_ENVIRONMENT === 'production'
const IS_STAGING_ENV = VITE_APP_ENVIRONMENT === 'staging'
const IS_DEVELOPMENT_ENV = VITE_APP_ENVIRONMENT === 'development'
const IS_LOCAL_ENV =
  !IS_PRODUCTION_ENV && !IS_STAGING_ENV && !IS_DEVELOPMENT_ENV

const IS_SENTRY_DISABLED = IS_LOCAL_ENV
const _RELEASE_NAME = IS_SENTRY_DISABLED
  ? ''
  : `${PROJECT_NAME}@${packageJson.version}${IS_PRODUCTION_ENV ? '' : `-${VITE_APP_ENVIRONMENT}`}`

const devProxy: ProxyOptions = {
  target: DEV_BACKEND,
  changeOrigin: true,
  configure: proxy => {
    proxy.on('proxyReq', proxyReq => {
      if (proxyReq.getHeader('origin'))
        proxyReq.setHeader('origin', new URL(DEV_BACKEND).origin)
    })
  }
}

// https://vitejs.dev/config/
export default defineConfig({
  // Served under a path prefix when VITE_BASE_PATH is set (e.g. "/s3-panel"
  // inside IAM's console); see src/lib/basePath.ts.
  base: `${(process.env.VITE_BASE_PATH || '').replace(/\/+$/, '')}/`,
  plugins: [react(), version()],
  // Every script, style and preload tag gets this placeholder as its nonce. The
  // backend replaces it per response with the nonce its CSP names
  // (internal/api/spa.go), so only the tags this build wrote may run.
  html: { cspNonce: '__CSP_NONCE__' },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src/')
    }
  },
  preview: {
    port: 3000,
    strictPort: true
  },
  server: {
    port: 8080,
    strictPort: true,
    host: true,
    origin: 'http://0.0.0.0:8080',
    /**
     * The SPA calls /api and /auth as same-origin relative paths and relies on
     * the session cookie, so in dev they have to reach the Go backend through
     * this origin. Without the proxy `iam` auth mode cannot work locally at all:
     * /api/config and /auth/me 404 against the dev server and the app falls back
     * to asking for S3 keys.
     *
     * Override the target with VITE_DEV_BACKEND when the backend is not on :8090.
     *
     * The backend refuses a state-changing request whose Origin names another
     * host, and this dev server is one. The proxy presents the backend's own
     * origin instead, which is the dev-only exception to that check.
     */
    proxy: {
      '/api': devProxy,
      '/auth': devProxy
    }
  },
  build: {
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[hash].js',
        chunkFileNames: 'assets/[hash].js',
        manualChunks: (id: string) => {
          if (id.includes('node_modules')) {
            if (id.includes('@tanstack/react-query')) {
              return 'vendor/react-query'
            }

            if (id.includes('@hookform/resolvers')) {
              return 'vendor/hookform'
            }

            return 'vendor/other'
          }
        }
      }
    },
    sourcemap: true
  }
})
