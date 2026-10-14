/**
 * What each page keeps in its URL.
 *
 * Page, search, filters and the open folder live in the URL rather than in
 * component state: leaving a page unmounts it, so state kept in the component
 * came back as page 1 of the unfiltered root on Back. Defaults are left out so
 * a plain URL stays plain.
 */

export interface IBucketsSearch {
  page?: number
  search?: string
  /** iam mode: show only buckets in this region. */
  region?: string
}

export interface IBucketObjectsSearch {
  /** The region the bucket was opened from; same-named buckets can exist in two. */
  region?: string
  /** The open folder, with its trailing slash. */
  path?: string
  page?: number
  search?: string
}

export interface IBucketDetailSearch {
  region?: string
}

// The router JSON-parses each value, so a search for "2024" arrives as a number.
const text = (value: unknown): string | undefined =>
  (typeof value === 'string' || typeof value === 'number') && value !== ''
    ? String(value)
    : undefined

const pageNumber = (value: unknown): number | undefined => {
  const page = Number(value)

  return Number.isInteger(page) && page > 1 ? page : undefined
}

export const parseBucketsSearch = (
  search: Record<string, unknown>
): IBucketsSearch => ({
  page: pageNumber(search.page),
  search: text(search.search),
  region: text(search.region)
})

export const parseBucketObjectsSearch = (
  search: Record<string, unknown>
): IBucketObjectsSearch => ({
  region: text(search.region),
  path: text(search.path),
  page: pageNumber(search.page),
  search: text(search.search)
})

export const parseBucketDetailSearch = (
  search: Record<string, unknown>
): IBucketDetailSearch => ({
  region: text(search.region)
})
