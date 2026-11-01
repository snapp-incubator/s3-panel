import { basePath } from '@/lib/basePath'
import { storageKey } from '@/lib/storageKey'
import { BaseHeaders } from './config'
import {
  type FetchError,
  type HTTPClientError,
  interceptorRequestConfig,
  interceptorResponseErrorConfig,
  type RequestHeaders
} from './interceptorsConfig'

export type Env = 'prod' | 'stage'

export type Region = string

export interface FetchOptions<TBody = unknown>
  extends Omit<RequestInit, 'body'> {
  headers?: Partial<RequestHeaders>
  body?: TBody
  retryCount?: number
}

export interface FetchResponse<TData = unknown> {
  data: TData
  status: number
  ok: boolean
}

const config = {
  // An explicit backend URL wins; otherwise requests go to the panel's own
  // backend, under whatever prefix the panel is served at.
  baseUrl: (import.meta.env.VITE_CENTRAL_BACKEND_API as string) || basePath,
  currentRegion:
    (localStorage.getItem(storageKey('currentRegion')) as Region) || 'teh-1',
  defaultHeaders: BaseHeaders()
}

export const updateRegion = (
  region: Region,
  extraHeaders?: Partial<RequestHeaders>
) => {
  config.currentRegion = region
  config.defaultHeaders.region = region
  localStorage.setItem(storageKey('currentRegion'), region)

  if (extraHeaders) Object.assign(config.defaultHeaders, extraHeaders)
}

export const getCurrentRegion = (): Region => config.currentRegion

function isFetchError(error: unknown): error is FetchError {
  return (
    typeof error === 'object' &&
    error !== null &&
    'status' in error &&
    'data' in error
  )
}

async function request<TData, TBody = unknown>(
  method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE',
  path: string,
  options: FetchOptions<TBody> = {}
): Promise<FetchResponse<TData>> {
  const { body, headers, retryCount = 0, ...rest } = options

  const mergedHeaders: Record<string, string> = Object.fromEntries(
    Object.entries({ ...config.defaultHeaders, ...headers }).map(([k, v]) => [
      k,
      String(v)
    ])
  )

  const isFormData = body instanceof FormData
  const fetchBody = isFormData ? body : body ? JSON.stringify(body) : undefined

  const { finalUrl, finalOptions } = interceptorRequestConfig(
    path,
    { ...rest, body: fetchBody, headers: mergedHeaders, method },
    config.baseUrl,
    mergedHeaders
  )

  try {
    const response = await fetch(finalUrl, finalOptions)

    await interceptorResponseErrorConfig(response)

    const json = (await response.json().catch(() => null)) as TData

    return { data: json, status: response.status, ok: response.ok }
  } catch (error) {
    if (retryCount > 0) {
      return request<TData, TBody>(method, path, {
        ...options,
        retryCount: retryCount - 1
      })
    }

    if (isFetchError(error)) {
      const httpError: HTTPClientError = {
        name: 'HTTPError',
        message: error.message,
        response: {
          data: error.data,
          status: error.status,
          statusText: error.statusText,
          headers: error.headers,
          config: finalOptions,
          url: error.url
        },
        config: finalOptions,
        toJSON(this: HTTPClientError) {
          return {
            message: this.message,
            status: this.response.status,
            data: this.response.data
          }
        }
      }

      throw httpError
    }

    throw error
  }
}

export async function uploadRequest<T>(
  path: string,
  body: FormData,
  headers?: Partial<RequestHeaders>,
  onUploadProgress?: (percent: number) => void
): Promise<FetchResponse<T>> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()

    xhr.open('POST', `${config.baseUrl}${path}`)

    const mergedHeaders = { ...config.defaultHeaders, ...headers }

    Object.entries(mergedHeaders).forEach(([key, value]) => {
      if (key.toLowerCase() !== 'content-type') {
        xhr.setRequestHeader(key, String(value))
      }
    })

    xhr.upload.onprogress = event => {
      if (event.lengthComputable && onUploadProgress) {
        const percent = (event.loaded / event.total) * 100

        onUploadProgress(percent)
      }
    }

    xhr.onerror = () => {
      const httpError: HTTPClientError = {
        name: 'HTTPError',
        message: 'Upload request failed',
        response: {
          data: null,
          status: 0,
          statusText: '',
          headers: {},
          config: {},
          url: `${config.baseUrl}${path}`
        },
        config: {},
        toJSON(this: HTTPClientError) {
          return {
            message: this.message,
            status: this.response.status,
            data: this.response.data
          }
        }
      }

      reject(httpError)
    }

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve({
            data: JSON.parse(xhr.responseText) as T,
            status: xhr.status,
            ok: true
          })
        } catch {
          resolve({
            data: xhr.responseText as unknown as T,
            status: xhr.status,
            ok: true
          })
        }
      } else {
        const httpError: HTTPClientError = {
          name: 'HTTPError',
          message: `Upload failed with status ${xhr.status}`,
          response: {
            data: xhr.responseText,
            status: xhr.status,
            statusText: xhr.statusText,
            headers: {},
            config: {},
            url: `${config.baseUrl}${path}`
          },
          config: {},
          toJSON(this: HTTPClientError) {
            return {
              message: this.message,
              status: this.response.status,
              data: this.response.data
            }
          }
        }

        reject(httpError)
      }
    }

    xhr.send(body)
  })
}

const centralClient = {
  get: <T>(path: string, options?: FetchOptions) =>
    request<T>('GET', path, options),
  post: <T, B>(path: string, body: B, options?: FetchOptions) =>
    request<T, B>('POST', path, { ...options, body }),
  put: <T, B>(path: string, body: B, options?: FetchOptions) =>
    request<T, B>('PUT', path, { ...options, body }),
  patch: <T, B>(path: string, body: B, options?: FetchOptions) =>
    request<T, B>('PATCH', path, { ...options, body }),
  delete: <T>(path: string, options?: FetchOptions) =>
    request<T>('DELETE', path, options),
  upload: uploadRequest
}

export default centralClient
