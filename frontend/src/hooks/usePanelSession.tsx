import { create } from 'zustand'

import { fetchPanelConfig, fetchPanelSession } from '@/api/panel'
import type { IPanelSessionResponse, TAuthMode } from '@/types/s3/panel.types'

interface IPanelSessionStore {
  /** null until the panel config has been read. */
  authMode: TAuthMode | null
  loginUrl: string
  session: IPanelSessionResponse | null
  loading: boolean
  /** Reads /api/config and, in iam mode, /auth/me. Safe to call repeatedly. */
  bootstrap: () => Promise<void>
  isIAMMode: () => boolean
  isAuthenticated: () => boolean
  isAdmin: () => boolean
  logout: () => Promise<void>
}

/**
 * Holds which authentication mode this deployment runs and, in `iam` mode, who
 * is signed in.
 *
 * Deliberately separate from `useS3Credentials`: that store holds credentials
 * the user typed, this one holds an identity the server established. Only one is
 * ever active, and the panel picks between them from the server's own config
 * rather than from a build-time flag, so the same bundle serves both modes.
 */
const usePanelSession = create<IPanelSessionStore>((set, get) => ({
  authMode: null,
  loginUrl: '',
  session: null,
  loading: false,

  bootstrap: async () => {
    if (get().loading) return
    set({ loading: true })

    try {
      const config = await fetchPanelConfig()

      if (config.auth_mode !== 'iam') {
        set({
          authMode: config.auth_mode,
          loginUrl: config.login_url ?? '',
          session: null
        })

        return
      }

      // A 401 here is the ordinary "not signed in yet" state, not a failure.
      const session = await fetchPanelSession()

      // authMode and session are published TOGETHER, in one update. The app
      // un-gates rendering as soon as authMode is known, and the router runs its
      // route guards immediately — so publishing authMode first would have the
      // guard read a session that has not arrived yet and bounce a signed-in
      // user straight back to the login screen.
      set({
        authMode: config.auth_mode,
        loginUrl: config.login_url ?? '',
        session
      })
    } catch {
      // Fall back to the historical behaviour rather than blocking the app: an
      // older backend has no /api/config, and it only speaks s3-credential login.
      set({ authMode: 's3', loginUrl: '', session: null })
    } finally {
      set({ loading: false })
    }
  },

  isIAMMode: () => get().authMode === 'iam',
  isAuthenticated: () => get().session?.authenticated === true,
  isAdmin: () => get().session?.is_admin === true,

  logout: async () => {
    await fetch('/auth/logout', { method: 'POST', credentials: 'include' })
    set({ session: null })
    window.location.href = '/'
  }
}))

export default usePanelSession
