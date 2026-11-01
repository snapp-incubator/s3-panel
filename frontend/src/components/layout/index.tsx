import type { PropsWithChildren } from 'react'

import Header from '@/components/layout/header'
import PanelBar from '@/components/layout/panel-bar'
import { Toaster } from '@/components/shadcn/toaster'
import Sidebar from '@/components/side-navbar'
import usePanelHeader from '@/hooks/usePanelHeader'
import usePanelSession from '@/hooks/usePanelSession'

export default function Layout({ children }: PropsWithChildren) {
  const inConsole = usePanelSession(state => state.panel !== null)
  const headerState = usePanelHeader(inConsole)

  if (!inConsole) {
    return (
      <>
        <Header />
        <div className="flex h-screen border-collapse overflow-hidden pt-10">
          <Sidebar />
          <main className="flex-1 overflow-y-auto overflow-x-hidden bg-secondary/10 px-5 pb-1 pt-10">
            {children}
            <Toaster />
          </main>
        </div>
      </>
    )
  }

  // Part of a console: its 56 px header sits above the app, in normal flow,
  // and the app fills what is left. Until the header reports, nothing is drawn
  // in its place, so a header that is merely quick does not flash the app's
  // own bar.
  return (
    <div className="flex h-[calc(100dvh-56px)] flex-col">
      {headerState === 'fallback' ? <PanelBar /> : null}
      <div className="flex min-h-0 flex-1 border-collapse overflow-hidden">
        <Sidebar className="h-full" />
        <main className="flex-1 overflow-y-auto overflow-x-hidden bg-secondary/10 px-5 pb-1 pt-6">
          {children}
          <Toaster />
        </main>
      </div>
    </div>
  )
}
