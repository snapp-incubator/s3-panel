import { storageKey } from '@/lib/storageKey'

export interface RequestHeaders {
  'Content-Type': string
  Authorization?: string
  access_key?: string
  secret_key?: string
  env: 'prod' | 'stage'
  region: string
}

export const interceptorRequestConfig = (
  url: string,
  options: RequestInit,
  currentBaseURL: string,
  currentHeaders: Record<string, string>
): { finalUrl: string; finalOptions: RequestInit } => {
  const finalUrl = `${currentBaseURL}${url}`

  const finalOptions: RequestInit = {
    ...options,
    headers: {
      ...currentHeaders,
      ...(options.headers || {})
    }
  }

  return { finalUrl, finalOptions }
}

export interface FetchError {
  message: string
  status: number
  statusText: string
  data: unknown
  headers: Record<string, string>
  url: string
  ok: boolean
  type: ResponseType
  redirected: boolean
}

export interface HTTPClientError<T = unknown> {
  name: string
  message: string
  response: {
    data: T
    status: number
    statusText: string
    headers: Record<string, string>
    config: RequestInit
    url: string
  }
  config: RequestInit
  toJSON(): { message: string; status: number; data: T }
}

export const interceptorResponseErrorConfig = async (
  response: Response
): Promise<Response> => {
  if (!response.ok) {
    let parsedData: unknown = null
    let rawText = ''

    try {
      rawText = await response.text()

      try {
        parsedData = JSON.parse(rawText)
      } catch {
        parsedData = rawText
      }
    } catch {
      parsedData = null
    }

    const errorResponse: FetchError = {
      message: `Request failed with status ${response.status}`,
      status: response.status,
      statusText: response.statusText,
      data: parsedData,
      headers: Object.fromEntries(response.headers.entries()),
      url: response.url,
      ok: response.ok,
      type: response.type,
      redirected: response.redirected
    }

    console.error('[Fetch Error]', errorResponse)
    reloadIfAccessRevoked(response.status, parsedData)
    throw errorResponse
  }

  return response
}

/** The reason the backend gives when the console revoked access to this app. */
const PANEL_VIEW_DENIED = 'panel_view_denied'

/**
 * Losing access to the app mid-session turns every call into a 403. Reloading
 * hands the page back to the server, which answers with its "No access" page
 * instead of the app. At most once a minute, so a refusal the page itself does
 * not see cannot turn into a reload loop.
 */
const reloadIfAccessRevoked = (status: number, data: unknown) => {
  if (
    status !== 403 ||
    typeof data !== 'object' ||
    data === null ||
    (data as { reason?: unknown }).reason !== PANEL_VIEW_DENIED
  )
    return

  const key = storageKey('panel-access-reload')
  try {
    const last = Number(sessionStorage.getItem(key) ?? 0)
    if (Date.now() - last < 60_000) return
    sessionStorage.setItem(key, String(Date.now()))
  } catch {
    return
  }
  window.location.reload()
}
