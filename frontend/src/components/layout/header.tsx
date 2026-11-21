import { Link, useMatchRoute } from '@tanstack/react-router'
import { Boxes } from 'lucide-react'

import { ThemeToggle } from '@/components/layout/theme-toggle'
import { useTitle } from '@/components/providers/titleProvider'
import useS3Credentials from '@/hooks/useS3Credentials'

import UserDetails from './user-details'

// lucide-react v1 dropped brand icons (incl. GitHub) for trademark reasons,
// so the GitHub mark is inlined here as an SVG.
const GithubIcon = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 24 24"
    fill="currentColor"
    aria-hidden="true"
    className={className}
  >
    <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222 0 1.606-.014 2.898-.014 3.293 0 .322.216.694.825.576C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
  </svg>
)

const Header = () => {
  const { title } = useTitle()
  const { isLogin } = useS3Credentials()

  const matchRoute = useMatchRoute()

  const isObjectStorageRoute = matchRoute({
    to: '/object-storage/s3-bucket',
    fuzzy: true
  })

  return (
    <header className="supports-backdrop-blur:bg-background/60 fixed inset-x-0 top-0 z-20 border-b bg-background/95 backdrop-blur">
      <nav className="flex h-16 items-center justify-between px-4">
        <Link
          to="/"
          className="hidden items-center justify-between gap-2 md:flex"
        >
          <Boxes aria-hidden="true" className="size-6" />
          {/* Not a heading: each page has its own <h1>, and this bar is
              absent inside a console. */}
          <div className="flex items-center justify-center gap-2">
            <span className="text-lg font-semibold">{title}</span>
          </div>
        </Link>
        <div className="flex items-center gap-3">
          {isLogin() && isObjectStorageRoute ? <UserDetails /> : null}
          {import.meta.env.PACKAGE_VERSION ? (
            <span className="text-xs text-muted-foreground">
              v{import.meta.env.PACKAGE_VERSION}
            </span>
          ) : null}
          <a
            href="https://github.com/snapp-incubator/S3-Panel"
            target="_blank"
            rel="noreferrer noopener"
            aria-label="Source code on GitHub"
            title="Source code on GitHub"
            className="text-muted-foreground transition-colors hover:text-foreground"
          >
            <GithubIcon className="size-5" />
          </a>
          <ThemeToggle />
        </div>
      </nav>
    </header>
  )
}

export default Header
