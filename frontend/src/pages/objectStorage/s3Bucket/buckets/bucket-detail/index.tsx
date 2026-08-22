import { useQuery } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useEffectOnce } from 'react-use'

import { fetchIAMBucketDetail } from '@/api/panel'
import { AlertMessage } from '@/components/alert-message'
import PermissionBadges from '@/components/bucket-card/permission-badges'
import ErrorState from '@/components/error-state'
import { useTitle } from '@/components/providers/titleProvider'
import { Card, CardContent, CardTitle } from '@/components/shadcn/card'
import { Skeleton } from '@/components/shadcn/skeleton'
import { t } from '@/i18n'

/** Renders a byte count for humans. -1 means "no limit", as the gateway reports. */
const formatBytes = (bytes: number): string => {
  if (bytes < 0) return t('no_quota')
  if (bytes === 0) return '0 B'

  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const exponent = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1
  )

  return `${(bytes / 1024 ** exponent).toFixed(exponent === 0 ? 0 : 2)} ${units[exponent]}`
}

const Stat = ({ label, value }: { label: string; value: string }) => (
  <div className="flex flex-col gap-1 rounded-lg border bg-card p-4">
    <span className="text-xs uppercase tracking-wide text-muted-foreground">
      {label}
    </span>
    <span className="text-2xl font-medium">{value}</span>
  </div>
)

/**
 * Bucket detail: how much is in the bucket, and who else can reach it.
 *
 * `iam` mode only — the numbers come from the storage backend's admin API (S3
 * has no verb reporting object count or size) and the policy is only readable
 * through the control endpoint.
 */
export default function BucketDetail() {
  const { setTitle } = useTitle()
  const { bucketName } = useParams({
    from: '/object-storage/s3-bucket/buckets/$bucketName/detail'
  })

  useEffectOnce(() => {
    setTitle(`SnappCloud - ${bucketName}`)
  })

  const {
    data: detail,
    isFetching,
    isError
  } = useQuery({
    queryFn: () => fetchIAMBucketDetail(bucketName),
    queryKey: ['bucket-detail', bucketName]
  })

  if (isError) {
    return <ErrorState />
  }

  if (isFetching || !detail) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-10 w-64" />
        <div className="grid gap-4 md:grid-cols-3">
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
          <Skeleton className="h-24" />
        </div>
        <Skeleton className="h-64" />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h2 className="text-3xl">{detail.display_name}</h2>
        <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
          {detail.region ? <span>{detail.region}</span> : null}
          {detail.tenant ? <span>{detail.tenant}</span> : null}
          {/* The gateway addresses a tenanted bucket differently from the panel,
              so anyone reaching for the CLI needs the gateway's spelling. */}
          {detail.s3_name && detail.s3_name !== detail.bucket ? (
            <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
              {detail.s3_name}
            </code>
          ) : null}
        </div>
        <PermissionBadges
          permissions={detail.permissions}
          grantedVia={detail.granted_via}
        />
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Stat
          label={t('objects_count')}
          value={detail.num_objects.toLocaleString()}
        />
        <Stat label={t('bucket_size')} value={formatBytes(detail.size_bytes)} />
        <Stat
          label={t('bucket_quota')}
          value={formatBytes(detail.quota_bytes)}
        />
      </div>

      <Card className="p-6">
        <CardTitle>{t('bucket_policy')}</CardTitle>
        <CardContent className="px-0 pt-4">
          {detail.policy_denied ? (
            <AlertMessage
              title={t('bucket_policy')}
              message={t('bucket_policy_denied')}
            />
          ) : detail.policy_present ? (
            <pre
              data-test="bucket-policy"
              className="max-h-[28rem] overflow-auto rounded-lg bg-muted p-4 text-xs"
            >
              {JSON.stringify(detail.policy, null, 2)}
            </pre>
          ) : (
            <AlertMessage
              title={t('bucket_policy')}
              message={t('no_bucket_policy')}
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}
