import { useSearch } from '@tanstack/react-router'
import { LogIn } from 'lucide-react'
import { useEffect } from 'react'

import { Button } from '@/components/shadcn/button'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'
import { panelLoginUrl } from '@/lib/basePath'
import { storageKey } from '@/lib/storageKey'
import type { ISignInSearch } from '@/router/search'

const AUTO_SIGN_IN_MS = 30_000

/** Reports whether an automatic sign-in may start now, and records that it did. */
const claimAutoSignIn = (): boolean => {
  const key = storageKey('panel-auto-sign-in')
  try {
    if (Date.now() - Number(sessionStorage.getItem(key) ?? 0) < AUTO_SIGN_IN_MS)
      return false
    sessionStorage.setItem(key, String(Date.now()))
    return true
  } catch {
    return false
  }
}

/**
 * The sign-in prompt for `iam` mode.
 *
 * There is no form here on purpose: the panel never sees the user's password or
 * their storage credentials. It hands the browser to the identity provider and
 * receives an identity back.
 */
const OIDCLogin = () => {
  const loginUrl = usePanelSession(state => state.loginUrl)
  const inConsole = usePanelSession(state => state.panel !== null)
  const { next } = useSearch({ strict: false }) as ISignInSearch

  // Part of a console, the person signed in once for every app: going straight
  // to the provider is a silent round trip while that sign-in lives. Once per
  // AUTO_SIGN_IN_MS, so a sign-in that does not stick (a cookie the browser
  // refuses, say) leaves the button here instead of looping.
  useEffect(() => {
    if (inConsole && claimAutoSignIn())
      window.location.href = panelLoginUrl(loginUrl, next)
  }, [inConsole, loginUrl, next])

  return (
    <div className="flex w-full flex-col items-center gap-6 py-12">
      <div className="flex flex-col items-center gap-2 text-center">
        <h2 className="text-2xl font-medium">{t('sign_in_title')}</h2>
        <p className="max-w-md text-muted-foreground">
          {t('sign_in_description')}
        </p>
      </div>

      <Button
        data-test="oidc-login-button"
        size="lg"
        // A full navigation, not fetch: the provider responds with redirects and
        // sets cookies on its own origin, neither of which survives an XHR.
        onClick={() => {
          window.location.href = panelLoginUrl(loginUrl, next)
        }}
      >
        <LogIn className="mr-2 size-4" />
        {t('sign_in_button')}
      </Button>
    </div>
  )
}

export default OIDCLogin
