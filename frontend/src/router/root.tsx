import { createRootRoute, Outlet } from '@tanstack/react-router'

import ErrorPage from '@/components/error-page'
import Layout from '@/components/layout'
import { ThemeProvider } from '@/components/providers/themeProvider'
import { TitleProvider } from '@/components/providers/titleProvider'
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

export const rootRoute = createRootRoute({
  component: RootComponent,
  notFoundComponent: () => {
    return (
      <ErrorPage
        title="404"
        subtitle="The page you are looking for was not found!"
        buttonText="Back to Home"
        buttonLink="/"
      />
    )
  }
})
