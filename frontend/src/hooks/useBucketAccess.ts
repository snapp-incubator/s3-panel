import { useQuery, useQueryClient } from '@tanstack/react-query'

import { fetchIAMBuckets } from '@/api/panel'
import usePanelSession from '@/hooks/usePanelSession'
import { retryUnlessDenied } from '@/lib/requestError'
import type { IIAMBucket, IIAMBucketListResponse } from '@/types/s3/panel.types'

/** The bucket list's own query key prefix (see the buckets page). */
const IAM_BUCKETS_KEY = 'iam-buckets'

/** Pages walked when looking a bucket up by name; a name search rarely needs two. */
const LOOKUP_PAGE_SIZE = 100
const LOOKUP_MAX_PAGES = 5

const matches = (bucket: IIAMBucket, name: string, region?: string) =>
  bucket.bucket === name && (!region || bucket.region === region)

/**
 * Looks the bucket up in the bucket list, the same answer IAM gives the list
 * page. Null when it is not there: then the caller has no access to it.
 */
const lookupIAMBucket = async (
  name: string,
  region?: string
): Promise<IIAMBucket | null> => {
  for (let page = 1; page <= LOOKUP_MAX_PAGES; page++) {
    const list = await fetchIAMBuckets(LOOKUP_PAGE_SIZE, page, name, region)
    const found = list.items.find(bucket => matches(bucket, name, region))

    if (found) return found
    if (page >= list.total_pages) break
  }

  return null
}

export interface IBucketAccess {
  /** Upload, delete and other object writes are offered. */
  canWrite: boolean
  /** The deployment refuses every write (`server.read_only`). */
  readOnly: boolean
  /** IAM says this person may only read this bucket (iam mode only). */
  bucketReadOnly: boolean
  /** The bucket as IAM lists it, when known (iam mode only). */
  bucket: IIAMBucket | null | undefined
}

/**
 * What the signed-in person may do with one bucket.
 *
 * In iam mode the permission is the one the bucket list reports (`can_write`),
 * read from the list already in the cache when the bucket was opened from it,
 * and asked of the same API otherwise (a link opened directly). In s3 mode the
 * panel cannot know what the typed keys allow, so writes are offered and the
 * gateway decides. Either way a read-only deployment offers no writes.
 *
 * While the permission is still being looked up, writes are not offered; if
 * the lookup fails they are, since the server enforces the permission anyway.
 */
const useBucketAccess = (name: string, region?: string): IBucketAccess => {
  const queryClient = useQueryClient()
  const isIAMMode = usePanelSession(state => state.authMode === 'iam')
  const readOnly = usePanelSession(state => state.readOnly)

  const cached = () => {
    const lists = queryClient.getQueriesData<IIAMBucketListResponse>({
      queryKey: [IAM_BUCKETS_KEY]
    })

    for (const [key, list] of lists) {
      const found = list?.items.find(bucket => matches(bucket, name, region))

      if (found) {
        return {
          bucket: found,
          updatedAt: queryClient.getQueryState(key)?.dataUpdatedAt
        }
      }
    }

    return undefined
  }

  const {
    data: bucket,
    isError,
    isPending
  } = useQuery({
    queryKey: ['iam-bucket-access', name, region ?? ''],
    queryFn: () => lookupIAMBucket(name, region),
    enabled: isIAMMode,
    initialData: () => cached()?.bucket,
    initialDataUpdatedAt: () => cached()?.updatedAt,
    staleTime: 60_000,
    retry: retryUnlessDenied
  })

  if (!isIAMMode) {
    return {
      canWrite: !readOnly,
      readOnly,
      bucketReadOnly: false,
      bucket: undefined
    }
  }

  const bucketReadOnly = !!bucket && !bucket.can_write
  const permitted = isError || (!isPending && !!bucket?.can_write)

  return {
    canWrite: !readOnly && permitted,
    readOnly,
    bucketReadOnly,
    bucket
  }
}

export default useBucketAccess
