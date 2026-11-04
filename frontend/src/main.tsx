// First: zod must be configured before any module builds a schema.
import '@/lib/zod'

import * as Sentry from '@sentry/react'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import App from '@/App.tsx'
import { router } from '@/router'

import '@/index.css'

import QueryClientProvider from './components/providers/queryClientProvider'

const ENVIRONMENT = import.meta.env.VITE_APP_ENVIRONMENT as string
const APP_VERSION = import.meta.env.PACKAGE_VERSION as string
const IS_PRODUCTION_ENV = ENVIRONMENT === 'production'
const RELEASE_NAME = `unifyPanel-pwa@${APP_VERSION}${IS_PRODUCTION_ENV ? '' : `-${ENVIRONMENT}`}`

Sentry.init({
  dsn: '',
  environment: ENVIRONMENT,
  dist: ENVIRONMENT,
  release: RELEASE_NAME,
  transport: Sentry.makeBrowserOfflineTransport(Sentry.makeFetchTransport),
  integrations: [
    Sentry.browserTracingIntegration(),
    Sentry.extraErrorDataIntegration(),
    Sentry.tanstackRouterBrowserTracingIntegration(router)
  ],
  // Performance Monitoring
  tracesSampleRate: 1.0,
  // Sentry v11 collects cookies, headers and bodies unless told otherwise; keep the v10 baseline.
  dataCollection: {
    userInfo: false,
    cookies: false,
    httpHeaders: {
      request: { deny: ['forwarded', '-ip', 'remote-', 'via', '-user'] },
      response: { deny: ['forwarded', '-ip', 'remote-', 'via', '-user'] }
    },
    httpBodies: [],
    urlQueryParams: { deny: ['forwarded', '-ip', 'remote-', 'via', '-user'] }
  }
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider>
      <App />
    </QueryClientProvider>
  </StrictMode>
)
