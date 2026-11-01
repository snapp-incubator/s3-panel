import { Boxes, LogOut } from 'lucide-react'

import { Button } from '@/components/shadcn/button'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'

/**
 * The app's own minimal bar, shown in place of the console's header when that
 * header fails or is slow (proposal 015 §5): the app's name and a sign-out.
 * The same 56 px as the header, in normal flow, so the page below does not
 * move when one replaces the other.
 */
const PanelBar = () => {
  const logout = usePanelSession(state => state.logout)
  const signedIn = usePanelSession(state => state.isAuthenticated())

  return (
    <header className="flex h-14 shrink-0 items-center justify-between border-b bg-background px-4">
      <div className="flex items-center gap-2">
        <Boxes className="size-5" />
        <span className="font-semibold">{t('app_name')}</span>
      </div>
      {signedIn ? (
        <Button variant="ghost" size="sm" onClick={() => void logout()}>
          <LogOut className="mr-2 size-4" />
          {t('sign_out')}
        </Button>
      ) : null}
    </header>
  )
}

export default PanelBar
