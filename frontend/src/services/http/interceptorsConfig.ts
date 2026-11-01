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
    throw errorResponse
  }

  return response
}
