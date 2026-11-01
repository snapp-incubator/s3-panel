import type { RequestHeaders } from './interceptorsConfig'

const AUTH_TOKEN: string = import.meta.env.VITE_AUTH_TOKEN as string
const ENV = (import.meta.env.VITE_ENV as 'stage' | 'prod') || 'prod'

export const BaseHeaders = (): RequestHeaders => ({
  'Content-Type': 'application/json',
  Authorization: `Bearer ${AUTH_TOKEN}`,
  env: ENV,
  region: 'teh-1'
})
