import path from 'node:path'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
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

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), version()],
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
     */
    proxy: {
      '/api': { target: DEV_BACKEND, changeOrigin: true },
      '/auth': { target: DEV_BACKEND, changeOrigin: true }
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
