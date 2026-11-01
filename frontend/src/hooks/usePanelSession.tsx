import { create } from 'zustand'

import {
  fetchPanelConfig,
  fetchPanelSession,
  PanelRequestError
} from '@/api/panel'
import { withBase } from '@/lib/basePath'
import type {
  IPanelConsole,
  IPanelSessionResponse,
  TAuthMode
} from '@/types/s3/panel.types'

interface IPanelSessionStore {
  /** null until the panel config has been read. */
  authMode: TAuthMode | null
  loginUrl: string
  /** Server refuses every mutating operation; hide the controls. */
  readOnly: boolean
  /** Where bucket cards link to manage access; empty hides the link. */
  iamConsoleUrl: string
  /** The unified console this panel is part of, or null when standalone. */
  panel: IPanelConsole | null
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
  iamConsoleUrl: '',
  panel: null,
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
            iamConsoleUrl: '',
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
          iamConsoleUrl: config.iam_console_url ?? '',
          panel: config.panel ?? null,
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
            iamConsoleUrl: '',
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
    // Best effort: the console's sign-out page signs out of this app too, so a
    // failure here must not stop the person reaching it.
    await fetch(withBase('/auth/logout'), {
      method: 'POST',
      credentials: 'include'
    }).catch(() => undefined)
    set({ session: null })
    // Part of a console, signing out means signing out of every app in it.
    window.location.href = get().panel?.signout_url ?? withBase('/')
  }
}))

export default usePanelSession
