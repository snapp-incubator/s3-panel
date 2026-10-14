import { create } from 'zustand'

import type { IBucketsSearch } from '@/router/search'

interface ILastBucketsSearchStore {
  search: IBucketsSearch
  remember: (search: IBucketsSearch) => void
}

/**
 * The page, search and region filter the bucket list was last left on, so a
 * link back to it from inside a bucket returns there instead of to page 1.
 */
const useLastBucketsSearch = create<ILastBucketsSearchStore>(set => ({
  search: {},
  remember: search => set({ search })
}))

export default useLastBucketsSearch
