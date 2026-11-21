import { UserCog } from 'lucide-react'

import { Button } from '@/components/shadcn/button'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'
import { manageAccessHref } from '@/lib/iamConsole'
import { cn } from '@/lib/utils'

interface ManageAccessLinkProps {
  /** The bucket's IAM resource ID, as the bucket list reports it. */
  resourceId?: string
  className?: string
  'data-test'?: string
}

/**
 * "Manage access": opens the bucket's page in the IAM console, where who has
 * access is changed. Renders nothing unless the deployment says where the
 * console is and IAM reported the bucket's resource ID — so a standalone or
 * s3-mode panel shows no link at all.
 */
export default function ManageAccessLink({
  resourceId,
  className,
  'data-test': dataTest = 'manage-access-link'
}: ManageAccessLinkProps) {
  const iamConsoleUrl = usePanelSession(state => state.iamConsoleUrl)
  const href = manageAccessHref(iamConsoleUrl, resourceId)

  if (!href) return null

  return (
    <Button
      asChild
      variant="ghost"
      size="sm"
      className={cn('shrink-0 gap-1 text-muted-foreground', className)}
    >
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        title={t('manage_access_hint')}
        data-test={dataTest}
      >
        <UserCog aria-hidden="true" className="size-3.5" />
        {t('manage_access')}
      </a>
    </Button>
  )
}
