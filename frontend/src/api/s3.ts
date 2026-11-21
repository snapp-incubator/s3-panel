import centralClient from '@/services/http/centralClient'
import { buildQueryString } from '@/services/http/query-client'
import type { IRegionsResponse } from '@/types/regions.types'
import type {
  IBucketObjectResponse,
  IBucketResponse,
  IBucketsListResponse,
  IUserDetailsResponse,
  IUserQuotaResponse
} from '@/types/s3/buckets.types'

/**
 * Options that pin a call to one region.
 *
 * In iam mode a bucket is its name AND its region — the same name can exist in
 * two — so every call for a bucket carries the region it was opened from, or an
 * empty one when that is unknown, which lets the server resolve the name alone.
 * Undefined leaves the client's default header in place, which in s3 mode is the
 * region the user signed in to.
 */
const inRegion = (region?: string) =>
  region === undefined ? undefined : { headers: { region } }

/** How many entries one page of an object listing holds. */
const OBJECTS_PAGE_SIZE = 20

const fetchRegions = async () => {
  const res = await centralClient.get<IRegionsResponse>('/s3/api/regions')

  return res.data
}

const createBucket = async ({ bucket }: { bucket: string }) => {
  const res = await centralClient.post('/s3/api/bucket/create', {
    bucket
  })

  return res
}

const fetchUserQuota = async () => {
  const res = await centralClient.get<IUserQuotaResponse>('/s3/api/user/quota')

  return res.data
}

interface IFetchObjectsParams {
  page: number
  searchValue?: string
  prefix?: string
  /** The next_continuation_token of the previous page; empty for page 1. */
  token?: string
  region?: string
}

/**
 * One page of a bucket's objects.
 *
 * Paged by continuation token: page N is fetched with the token page N-1 handed
 * back, because numbering pages means counting the whole bucket first.
 */
const fetchObjects = async (
  bucket: string,
  { page, searchValue, prefix, token, region }: IFetchObjectsParams
) => {
  const params: Record<string, string | number | undefined> = {
    bucket,
    max_keys: OBJECTS_PAGE_SIZE,
    page,
    search_string: searchValue || undefined,
    prefix: prefix || undefined,
    continuation_token: token || undefined
  }

  const res = await centralClient.get<IBucketObjectResponse>(
    `/s3/api/object/list?${buildQueryString(params)}`,
    inRegion(region)
  )

  return res.data
}

const fetchBucketsQuota = async (
  max_keys: number,
  page: number,
  searchValue?: string
) => {
  const params = {
    max_keys,
    page,
    search_string: searchValue || undefined
  }

  const res = await centralClient.get<IBucketResponse>(
    `/s3/api/bucket/quota?${buildQueryString(params)}`
  )

  return res.data
}

const fetchBucketsList = async () => {
  const res = await centralClient.get<IBucketsListResponse>(
    `/s3/api/bucket/list?${buildQueryString({
      max_keys: 10,
      page: 1
    })}`
  )

  return res
}

const fetchUserDetails = async () => {
  const res = await centralClient.get<IUserDetailsResponse>('/s3/api/user/id')

  return res.data
}

const deleteObject = async (
  bucket: string,
  objects: string[],
  region?: string
) => {
  const res = await centralClient.delete<{
    deleted: boolean
  }>(
    `/s3/api/object/delete?${buildQueryString({
      bucket,
      objects: objects.join(',')
    })}`,
    inRegion(region)
  )

  return res
}

const deleteBucketApi = async (bucket: string) => {
  const res = await centralClient.delete(
    `/s3/api/bucket/delete?${buildQueryString({ bucket })}`
  )

  return res
}

const uploadObjects = async (
  formData: FormData,
  onUploadProgress?: (percent: number) => void,
  region?: string
) => {
  const response = await centralClient.upload<{ created: boolean }>(
    '/s3/api/object/upload',
    formData,
    inRegion(region)?.headers,
    onUploadProgress
  )

  return response.data
}

const downloadObject = async (
  bucket: string,
  object: string,
  region?: string,
  signal?: AbortSignal
): Promise<{ url: string }> => {
  const response = await centralClient.get<{ url: string }>(
    `/s3/api/object/download?${buildQueryString({ bucket, object })}`,
    { ...inRegion(region), signal }
  )

  return response.data
}

interface IShareLinkResponse {
  url: string
  /**
   * When the link stops working (RFC 3339), after every cap the server applied —
   * not the duration asked for. Absent from a backend that predates it.
   */
  expires_at?: string
  /** How long the link was signed for, in seconds. */
  expires_in_seconds?: number
  /**
   * The link is signed with the person's session credential (iam mode), so it
   * also ends when they sign out. Absent from a backend that predates it.
   */
  session_bound?: boolean
}

interface IShareLimitResponse {
  /** The longest a link may live right now, in seconds; 0 means uncapped. */
  max_seconds: number
  /** A link would end when the person signs out (iam mode). */
  session_bound: boolean
}

/**
 * The longest share link the server would sign for this bucket right now: what
 * remains of the credential that signs it (iam mode) and share.max_duration.
 */
const fetchShareLimit = async (
  bucket: string,
  region?: string,
  signal?: AbortSignal
): Promise<IShareLimitResponse> => {
  const response = await centralClient.get<IShareLimitResponse>(
    `/s3/api/object/share/limit?${buildQueryString({ bucket })}`,
    { ...inRegion(region), signal }
  )

  return response.data
}

const shareLink = async (
  bucket: string,
  object: string,
  expiration: string,
  region?: string
): Promise<IShareLinkResponse> => {
  const params = {
    bucket,
    object,
    expiration
  }

  // POST: minting a link is an action, not a read.
  const response = await centralClient.post<IShareLinkResponse, undefined>(
    `/s3/api/object/share?${buildQueryString(params)}`,
    undefined,
    inRegion(region)
  )

  return response.data
}

export type { IShareLimitResponse, IShareLinkResponse }

export {
  createBucket,
  deleteBucketApi,
  deleteObject,
  downloadObject,
  fetchBucketsList,
  fetchBucketsQuota,
  fetchObjects,
  fetchRegions,
  fetchShareLimit,
  fetchUserDetails,
  fetchUserQuota,
  inRegion,
  OBJECTS_PAGE_SIZE,
  shareLink,
  uploadObjects
}
