import { useNavigate } from '@tanstack/react-router'
import { Database } from 'lucide-react'

import ManageAccessLink from '@/components/manage-access-link'
import { Button } from '@/components/shadcn/button'
import { t } from '@/i18n'
import type { IIAMBucket } from '@/types/s3/panel.types'

import PermissionBadges from './permission-badges'
import RegionBadge from './region-badge'

/**
 * A bucket card for `iam` mode.
 *
 * It is a separate component from the `s3`-mode card rather than a branch inside
 * it because the two carry different information: the `s3` card is built around
 * quota, which the listing supplies there, while here the listing supplies
 * permissions and region and quota needs a per-bucket call the detail page makes.
 */
const IAMBucketCard = (bucket: IIAMBucket) => {
  const navigate = useNavigate()

  // The region travels with the bucket: the same name can exist in two regions,
  // and every call made for this bucket has to say which one it means.
  const openObjects = () =>
    navigate({
      to: '/object-storage/s3-bucket/buckets/$bucketName',
      params: { bucketName: bucket.bucket },
      search: { region: bucket.region || undefined }
    })

  const openDetail = () =>
    navigate({
      to: '/object-storage/s3-bucket/buckets/$bucketName/detail',
      params: { bucketName: bucket.bucket },
      search: { region: bucket.region || undefined }
    })

  return (
    <div
      data-test="bucket-card"
      data-test-bucket-name={bucket.bucket}
      className="flex min-w-[300px] flex-col gap-4 rounded-xl border bg-card p-5 shadow-sm transition-shadow hover:shadow-md"
    >
      <div className="flex items-start gap-3">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-green-600/10 text-green-700">
          <Database className="size-5" />
        </div>

        <div className="min-w-0 flex-1">
          <p className="truncate font-medium" title={bucket.bucket}>
            {bucket.display_name}
          </p>

          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <RegionBadge region={bucket.region} />
            {bucket.tenant ? (
              <span className="truncate" title={bucket.tenant}>
                {bucket.tenant}
              </span>
            ) : null}
          </div>
        </div>

        {/* Who has access is managed in IAM, on the bucket's own page. */}
        <ManageAccessLink
          resourceId={bucket.resource_id}
          className="h-7 px-2 text-xs"
          data-test="bucket-manage-access-link"
        />
      </div>

      <PermissionBadges
        permissions={bucket.permissions}
        grantedVia={bucket.granted_via}
      />

      <div className="mt-auto flex gap-2">
        <Button
          variant="outline"
          size="sm"
          className="flex-1"
          onClick={openObjects}
        >
          {t('objects')}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          className="flex-1"
          data-test="bucket-detail-link"
          onClick={openDetail}
        >
          {t('bucket_detail')}
        </Button>
      </div>
    </div>
  )
}

export default IAMBucketCard
