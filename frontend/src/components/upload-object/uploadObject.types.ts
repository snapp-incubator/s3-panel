export type TUploadObjectProps = {
  bucketName: string
  /** The bucket's region; undefined keeps the signed-in region (s3 mode). */
  region?: string
  currentPath: string
  refetchObjects: () => void
}
