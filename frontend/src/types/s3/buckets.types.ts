export type TBucketResponse = {
  bucket: string
  quota_enabled: boolean
  used_bytes: number
  used_bytes_unit: string
  used_bytes_raw: number
  hard_bytes: number
  hard_bytes_unit: string
  hard_bytes_raw: number
  used_objects: number
  hard_objects: number
  modify_time_stamp: string
  tenant: string
  access: string
}

export interface IBucketResponse {
  items: TBucketResponse[]
  total_buckets: number
  total_pages: number
}

export interface IBucketObjectResponse {
  items: {
    name: string
    last_modified_timestamp: string
    size_unit: string
    size_value: number
    is_folder: boolean
  }[]
  /** Pages known to exist: this one, plus one when is_truncated. */
  total_pages: number
  total_matched_items?: number
  /** The listing continues past this page. */
  is_truncated: boolean
  /** Fetches the page after this one. Absent on the last page. */
  next_continuation_token?: string
}

export interface IUserQuotaResponse {
  hard_buckets: number
  hard_bytes: number
  hard_bytes_raw: number
  hard_bytes_unit: string
  hard_objects: number
  quota_enabled: boolean
  used_buckets: number
  used_bytes: number
  used_bytes_unit: string
  used_bytes_raw: number
  used_objects: number
}

export interface IUserDetailsResponse {
  display_name: string
  suspended: number
  team: string
  userID: string
  user_not_found: boolean
}

export interface IBucketsListResponse {
  bucket_list: string[]
}
