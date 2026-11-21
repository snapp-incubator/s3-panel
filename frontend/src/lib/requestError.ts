import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'

/**
 * What a failed panel call means for the person looking at the page.
 *
 * - `denied`: 403 or 404. They are deliberately not told apart: the backend
 *   answers "no such bucket" for a bucket the caller may not see, so buckets
 *   cannot be enumerated. Either way the next step is to ask for access.
 * - `unavailable`: no answer at all, or a 5xx. Usually transient; retry.
 * - `failed`: any other refusal (a 400, a 422, an expired session).
 */
export type TRequestErrorKind = 'denied' | 'unavailable' | 'failed'

/**
 * The HTTP status a failed call carried, whichever client made it: the central
 * client nests it under `response`, the panel's own calls carry it directly.
 * 0 when the request never got an answer (network failure, abort).
 */
export const errorStatus = (error: unknown): number => {
  if (typeof error !== 'object' || error === null) return 0

  const { response, status } = error as {
    response?: { status?: unknown }
    status?: unknown
  }

  if (typeof response?.status === 'number') return response.status
  if (typeof status === 'number') return status

  return 0
}

export const requestErrorKind = (error: unknown): TRequestErrorKind => {
  const status = errorStatus(error)

  if (status === 403 || status === 404) return 'denied'
  if (status === 0 || status >= 500) return 'unavailable'

  return 'failed'
}

/**
 * Retry policy for reads: a refusal will not change by asking again, so only
 * an outage is retried, once.
 */
export const retryUnlessDenied = (failureCount: number, error: unknown) =>
  failureCount < 1 && requestErrorKind(error) === 'unavailable'

/**
 * What the server said went wrong, when it said something a person can read.
 * Uploads go through XHR, which hands the body back as text, so a JSON body is
 * parsed here; anything else (a proxy's HTML error page) is not shown.
 */
export const serverMessage = (error: unknown): string | undefined => {
  if (typeof error !== 'object' || error === null) return undefined

  let data = (error as { response?: { data?: unknown } }).response?.data

  if (typeof data === 'string') {
    try {
      data = JSON.parse(data)
    } catch {
      return undefined
    }
  }

  const message =
    typeof data === 'object' && data !== null
      ? (data as { message?: unknown }).message
      : undefined

  return typeof message === 'string' && message.trim() ? message : undefined
}

/**
 * Whom to ask for access. In iam mode access is granted in IAM by the bucket's
 * team admins; in s3 mode it comes with the keys, from whoever issued them.
 */
export const whoToAsk = (isIAMMode: boolean): string =>
  isIAMMode ? t('ask_access_iam') : t('ask_access_s3')

/** Why the panel got no answer, in terms of what it could not reach. */
export const unreachableHint = (isIAMMode: boolean): string =>
  isIAMMode
    ? t('error_cant_reach_iam_hint')
    : t('error_cant_reach_storage_hint')

/**
 * Title and description for a toast about a refused action. A refusal says
 * whom to ask; an outage says it is worth trying again.
 */
export const errorToastContent = (
  error: unknown,
  fallbackTitle: string
): { title: string; description?: string } => {
  const isIAMMode = usePanelSession.getState().authMode === 'iam'
  const kind = requestErrorKind(error)

  return {
    title: serverMessage(error) ?? fallbackTitle,
    description:
      kind === 'denied'
        ? whoToAsk(isIAMMode)
        : kind === 'unavailable'
          ? unreachableHint(isIAMMode)
          : undefined
  }
}
