export type TDownloadObjectProps = {
  bucket: string
  object: string
  /** The bucket's region; undefined keeps the signed-in region (s3 mode). */
  region?: string
}
