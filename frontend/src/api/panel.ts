import centralClient from '@/services/http/centralClient'
import type {
  IIAMBucketDetailResponse,
  IIAMBucketListResponse,
  IPanelConfigResponse,
  IPanelSessionResponse
} from '@/types/s3/panel.types'

/**
 * Reads which authentication mode this deployment runs.
 *
 * Unauthenticated on purpose: the SPA has to know whether to render a credential
 * form or a sign-in button before it can authenticate at all.
 */
const fetchPanelConfig = async (): Promise<IPanelConfigResponse> => {
  const res = await fetch('/api/config', { credentials: 'include' })

  if (!res.ok) throw new Error(`panel config unavailable: ${res.status}`)

  return (await res.json()) as IPanelConfigResponse
}

/**
 * Reads the current OIDC session. A 401 is the ordinary "not signed in" state
 * and resolves to `{ authenticated: false }` rather than throwing.
 */
const fetchPanelSession = async (): Promise<IPanelSessionResponse> => {
  const res = await fetch('/auth/me', { credentials: 'include' })

  if (res.status === 401) return { authenticated: false }
  if (!res.ok) throw new Error(`session unavailable: ${res.status}`)

  return (await res.json()) as IPanelSessionResponse
}

/**
 * Lists the buckets the signed-in user may access.
 *
 * One call covers every region: in `iam` mode the control endpoint knows about
 * all of them, so the panel does not fan out per region the way `s3` mode must.
 */
const fetchIAMBuckets = async (
  maxKeys: number,
  page: number,
  searchValue?: string
): Promise<IIAMBucketListResponse> => {
  const params = new URLSearchParams({
    max_keys: String(maxKeys),
    page: String(page)
  })

  if (searchValue) params.set('search_string', searchValue)

  const res = await centralClient.get<IIAMBucketListResponse>(
    `/s3/api/bucket/list?${params.toString()}`
  )

  return res.data
}

/** Object count, size, quota and bucket policy for one bucket. */
const fetchIAMBucketDetail = async (
  bucket: string
): Promise<IIAMBucketDetailResponse> => {
  const params = new URLSearchParams({ bucket })

  const res = await centralClient.get<IIAMBucketDetailResponse>(
    `/s3/api/bucket/detail?${params.toString()}`
  )

  return res.data
}

export {
  fetchIAMBucketDetail,
  fetchIAMBuckets,
  fetchPanelConfig,
  fetchPanelSession
}
