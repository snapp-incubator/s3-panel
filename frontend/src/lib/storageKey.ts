import { basePath } from '@/lib/basePath'

/**
 * The prefix every localStorage and sessionStorage key carries.
 *
 * Browser storage belongs to the ORIGIN, not the path, so apps sharing a host
 * share it too: unprefixed, another app's `currentRegion` or theme would be
 * read as this panel's, and this panel's would overwrite theirs. The prefix is
 * the base path the panel is served under ("/storage" → "storage."), or
 * VITE_STORAGE_PREFIX when set. A panel at the host's root keeps its keys
 * unprefixed, exactly as before.
 *
 * Nothing kept here is authority: it is a convenience any script on the origin
 * can read and write.
 */
export const storagePrefix: string =
  (import.meta.env.VITE_STORAGE_PREFIX as string | undefined) ??
  (basePath ? `${basePath.slice(1).split('/').join('.')}.` : '')

export const storageKey = (name: string): string => `${storagePrefix}${name}`
