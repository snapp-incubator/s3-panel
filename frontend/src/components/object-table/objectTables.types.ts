import type { IBucketObjectResponse } from '@/types/s3/buckets.types'

export type TObjectTablesProps = {
  /** What the listing threw, or null; shown as an error state, not as empty. */
  error: unknown
  /** Lists again; offered by the error state for an outage. */
  onRetry: () => void
  isLoading: boolean
  bucket: string
  /** The bucket's region; undefined keeps the signed-in region (s3 mode). */
  region?: string
  currentPath: string
  objectList: IBucketObjectResponse
  isSearch: boolean
  /** Upload and delete are offered (permission and deployment allow it). */
  canWrite: boolean
  /** Share links are offered (refused on a read-only deployment). */
  canShare: boolean
  refetchObjects: () => void
  onShareObject: (objectName: string) => void
  onNavigateToFolder: (folderName: string) => void
}
