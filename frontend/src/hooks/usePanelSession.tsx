import { create } from 'zustand'

import {
  fetchPanelConfig,
  fetchPanelSession,
  PanelRequestError
} from '@/api/panel'
import type { IPanelSessionResponse, TAuthMode } from '@/types/s3/panel.types'

interface IPanelSessionStore {
  /** null until the panel config has been read. */
  authMode: TAuthMode | null
  loginUrl: string
  /** Server refuses every mutating operation; hide the controls. */
  readOnly: boolean
  session: IPanelSessionResponse | null
  loading: boolean
  /** Set when bootstrap could not reach the server; null once it succeeds. */
  bootstrapError: string | null
  /** Reads /api/config and, in iam mode, /auth/me. Safe to call repeatedly. */
  bootstrap: () => Promise<void>
  isIAMMode: () => boolean
  isReadOnly: () => boolean
  isAuthenticated: () => boolean
  isAdmin: () => boolean
  logout: () => Promise<void>
}

/**
 * The bootstrap currently running, if any.
 *
 * Concurrent callers await THIS rather than returning early. A `loading` flag
 * alone resolved the second caller's promise immediately, so whoever awaited it
 * carried on with `authMode` still null — the very state the await was there to
 * wait out.
 */
let inFlight: Promise<void> | null = null

/**
 * Holds which authentication mode this deployment runs and, in `iam` mode, who
 * is signed in.
 *
 * Deliberately separate from `useS3Credentials`: that store holds credentials
 * the user typed, this one holds an identity the server established. Only one is
 * ever active, and the panel picks between them from the server's own config
 * rather than from a build-time flag, so the same bundle serves both regimes.
 */
const usePanelSession = create<IPanelSessionStore>((set, get) => ({
  authMode: null,
  loginUrl: '',
  readOnly: false,
  session: null,
  loading: false,
  bootstrapError: null,

  bootstrap: async () => {
    if (inFlight) return inFlight

    const run = async () => {
      set({ loading: true })

      try {
        const config = await fetchPanelConfig()

        if (config.auth_mode !== 'iam') {
          set({
            authMode: config.auth_mode,
            loginUrl: config.login_url ?? '',
            readOnly: config.read_only === true,
            session: null,
            bootstrapError: null
          })

          return
        }

        // A 401 here is the ordinary "not signed in yet" state, not a failure.
        const session = await fetchPanelSession()

        // authMode and session are published TOGETHER, in one update. The app
        // un-gates rendering as soon as authMode is known, and the router runs
        // its route guards immediately — so publishing authMode first would have
        // the guard read a session that has not arrived yet and bounce a
        // signed-in user straight back to the login screen.
        set({
          authMode: config.auth_mode,
          loginUrl: config.login_url ?? '',
          readOnly: config.read_only === true,
          session,
          bootstrapError: null
        })
      } catch (error) {
        const status =
          error instanceof PanelRequestError ? error.status : undefined

        // Only a 404 means "this backend has no /api/config", which is an older
        // build that speaks nothing but s3-credential login. Everything else —
        // a 500, a gateway blip, no network — is a fault, and answering it by
        // switching to s3 mode showed a signed-in user a credential form they
        // have no keys for, on a deployment where keys are the thing the panel
        // exists to avoid.
        if (status === 404) {
          set({
            authMode: 's3',
            loginUrl: '',
            readOnly: false,
            session: null,
            bootstrapError: null
          })

          return
        }

        // Nothing about the mode is known any more, so nothing about it is
        // changed: whatever was last read stays, and the error is published for
        // the app to surface and offer a retry.
        set({
          bootstrapError:
            error instanceof Error ? error.message : 'could not reach the panel'
        })
      } finally {
        set({ loading: false })
      }
    }

    inFlight = run().finally(() => {
      inFlight = null
    })

    return inFlight
  },

  isIAMMode: () => get().authMode === 'iam',
  isReadOnly: () => get().readOnly,
  isAuthenticated: () => get().session?.authenticated === true,
  isAdmin: () => get().session?.is_admin === true,

  logout: async () => {
    await fetch('/auth/logout', { method: 'POST', credentials: 'include' })
    set({ session: null })
    window.location.href = '/'
  }
}))

export default usePanelSession
