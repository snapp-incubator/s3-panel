import { Moon, Sun } from 'lucide-react'
import { useTheme } from '@/components/providers/themeProvider'

import { Button } from '@/components/shadcn/button'
import { t } from '@/i18n'

export function ThemeToggle() {
  const { setTheme, theme } = useTheme()

  return (
    <Button
      onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}
      variant="ghost"
      size="icon"
      className="size-9 rounded-md border"
      aria-label={t('toggle_theme')}
      title={t('toggle_theme')}
    >
      <Sun
        aria-hidden="true"
        className="size-4 rotate-0 scale-100 transition-all dark:-rotate-90 dark:scale-0"
      />
      <Moon
        aria-hidden="true"
        className="absolute size-4 rotate-90 scale-0 transition-transform dark:rotate-0 dark:scale-100"
      />
    </Button>
  )
}
