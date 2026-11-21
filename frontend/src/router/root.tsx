import { createRootRoute, Outlet } from '@tanstack/react-router'

import ErrorPage from '@/components/error-page'
import Layout from '@/components/layout'
import { ThemeProvider } from '@/components/providers/themeProvider'
import { TitleProvider } from '@/components/providers/titleProvider'
import useDocumentTitle from '@/hooks/useDocumentTitle'
import { t } from '@/i18n'
import { storageKey } from '@/lib/storageKey'

const RootComponent = () => {
  return (
    <ThemeProvider
      defaultTheme="light"
      storageKey={storageKey('vite-ui-theme')}
    >
      <TitleProvider>
        <Layout>
          <Outlet />
        </Layout>
      </TitleProvider>
    </ThemeProvider>
  )
}

const NotFound = () => {
  useDocumentTitle(t('page_not_found'))

  return (
    <ErrorPage
      title="404"
      subtitle="The page you are looking for was not found!"
      buttonText="Back to Home"
      buttonLink="/"
    />
  )
}

export const rootRoute = createRootRoute({
  component: RootComponent,
  notFoundComponent: NotFound
})
