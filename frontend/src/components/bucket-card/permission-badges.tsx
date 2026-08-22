import { Users } from 'lucide-react'

import { t } from '@/i18n'

const badgeStyle: Record<string, string> = {
  read: 'bg-sky-500/10 text-sky-700 dark:text-sky-300',
  write: 'bg-amber-500/10 text-amber-700 dark:text-amber-300',
  owner: 'bg-violet-500/10 text-violet-700 dark:text-violet-300'
}

interface IPermissionBadgesProps {
  permissions: string[]
  /** Set when the access came from a team rather than a direct grant. */
  grantedVia?: string
}

/**
 * Shows what the signed-in user may do with a bucket.
 *
 * Only meaningful in `iam` mode: in `s3` mode a listed bucket is one the user's
 * own credentials own, so there is nothing to distinguish.
 */
const PermissionBadges = ({
  permissions,
  grantedVia
}: IPermissionBadgesProps) => {
  if (!permissions?.length) return null

  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {permissions.map(permission => (
        <span
          key={permission}
          data-test="bucket-permission"
          className={`rounded-md px-1.5 py-0.5 text-xs font-medium ${
            badgeStyle[permission.toLowerCase()] ??
            'bg-muted text-muted-foreground'
          }`}
        >
          {permission}
        </span>
      ))}

      {grantedVia ? (
        <span
          className="flex items-center gap-1 text-xs text-muted-foreground"
          title={t('granted_via_team').replace('{team}', grantedVia)}
        >
          <Users className="size-3" />
          {grantedVia}
        </span>
      ) : null}
    </div>
  )
}

export default PermissionBadges
