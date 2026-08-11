import { LogIn } from 'lucide-react'

import { Button } from '@/components/shadcn/button'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'

/**
 * The sign-in prompt for `iam` mode.
 *
 * There is no form here on purpose: the panel never sees the user's password or
 * their storage credentials. It hands the browser to the identity provider and
 * receives an identity back.
 */
const OIDCLogin = () => {
  const loginUrl = usePanelSession(state => state.loginUrl)

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
          window.location.href = loginUrl || '/auth/login'
        }}
      >
        <LogIn className="mr-2 size-4" />
        {t('sign_in_button')}
      </Button>
    </div>
  )
}

export default OIDCLogin
