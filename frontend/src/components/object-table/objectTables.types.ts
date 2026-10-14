import type { IBucketObjectResponse } from '@/types/s3/buckets.types'

export type TObjectTablesProps = {
  isError: boolean
  isLoading: boolean
  bucket: string
  /** The bucket's region; undefined keeps the signed-in region (s3 mode). */
  region?: string
  currentPath: string
  objectList: IBucketObjectResponse
  isSearch: boolean
  refetchObjects: () => void
  onShareObject: (objectName: string) => void
  onNavigateToFolder: (folderName: string) => void
}
