import { ThemeProvider } from '@/components/providers/themeProvider'
import { withBase } from '@/lib/basePath'
import { storageKey } from '@/lib/storageKey'
import type { ErrorPageProps } from './error.type'

export default function ErrorPage({
  title,
  subtitle,
  buttonText,
  buttonLink
}: ErrorPageProps) {
  return (
    <ThemeProvider
      defaultTheme="light"
      storageKey={storageKey('vite-ui-theme')}
    >
      {title}
      <p>{subtitle}</p>
      <a href={withBase(buttonLink)}>{buttonText}</a>
    </ThemeProvider>
  )
}
