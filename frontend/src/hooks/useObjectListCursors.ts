import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

/** How many listings keep their page tokens. Each is a few short strings. */
const MAX_LISTINGS = 30

interface IObjectListCursorsStore {
  /**
   * Per listing, the token each known page starts from: trail[0] is page 1's
   * (always empty), trail[n] is page n+1's.
   */
  trails: Record<string, string[]>
  /** Records what a loaded page said about the page after it. */
  recordPage: (listing: string, page: number, next?: string) => void
}

/** Identifies one listing: a folder of a bucket, optionally searched. */
export const listingKey = (
  region: string | undefined,
  bucket: string,
  path: string,
  search: string
) => JSON.stringify([region ?? '', bucket, path, search])

/**
 * Where each page of an object listing starts.
 *
 * The server pages objects by continuation token rather than by number —
 * numbering pages means counting the whole bucket, which is what hung large
 * ones — so page N can only be fetched with the token page N-1 handed back. The
 * URL carries the page number and this carries the tokens, in session storage,
 * so Back or a reload lands on the same page rather than on page 1.
 */
const useObjectListCursors = create<IObjectListCursorsStore>()(
  persist(
    set => ({
      trails: {},
      recordPage: (listing, page, next) =>
        set(state => {
          const known = state.trails[listing] ?? ['']

          // Unchanged: the page still continues from the same token, or still
          // ends the listing.
          if (next ? known[page] === next : known.length === page) return state

          // Anything recorded past this page described a listing that has since
          // changed, so it is dropped rather than trusted.
          const trail = known.slice(0, page)
          if (next) trail.push(next)

          const trails = Object.fromEntries(
            Object.entries(state.trails)
              .filter(([key]) => key !== listing)
              .slice(-(MAX_LISTINGS - 1))
          )
          trails[listing] = trail

          return { trails }
        })
    }),
    {
      name: 'object-list-cursors',
      storage: createJSONStorage(() => sessionStorage)
    }
  )
)

export default useObjectListCursors
