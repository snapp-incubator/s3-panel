/**
 * The path prefix the panel is served under, without a trailing slash: "" when
 * it owns its host, "/s3-panel" when it is mounted under another one (e.g. at a
 * prefix of IAM's console). Vite derives import.meta.env.BASE_URL from `base`,
 * which vite.config.ts takes from VITE_BASE_PATH at build time.
 *
 * Every root-absolute URL the panel itself serves must go through withBase —
 * otherwise, under a prefix, the request lands on the host's OTHER service
 * (/auth/login would start IAM's login, not the panel's).
 */
export const basePath: string = import.meta.env.BASE_URL.replace(/\/+$/, '')

export const withBase = (path: string): string =>
  path.startsWith('/') ? `${basePath}${path}` : path

/**
 * Where the sign-in button sends the browser. The backend reports its login URL
 * with the base path already on; an older backend reports a bare
 * "/auth/login", which takes the prefix here. `next` is the page to return to
 * afterwards.
 */
export const panelLoginUrl = (loginUrl: string, next?: string): string => {
  const url = loginUrl || '/auth/login'
  const login =
    basePath && (url === basePath || url.startsWith(`${basePath}/`))
      ? url
      : withBase(url)

  if (!next) return login

  return `${login}${login.includes('?') ? '&' : '?'}next=${encodeURIComponent(next)}`
}
