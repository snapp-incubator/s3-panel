import { useNavigate } from '@tanstack/react-router'
import { Copy, Database, Ellipsis } from 'lucide-react'
import { useState } from 'react'

import { Button } from '@/components/shadcn/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from '@/components/shadcn/dropdown-menu'
import { Progress } from '@/components/shadcn/progress'
import { useToast } from '@/hooks/use-toast'
import useDeleteBucket from '@/hooks/useDeleteBucket'
import useS3Credentials from '@/hooks/useS3Credentials'
import { t } from '@/i18n'
import { calculateValue, copyString, timeAgo } from '@/lib/utils'
import type { TBucketResponse } from '@/types/s3/buckets.types'

import DeleteConfirmation from '../delete-confirmation'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from '../shadcn/tooltip'

import DeleteBucket from './delete-bucket'
import RegionBadge from './region-badge'

const usageColor = (pct: number) => {
  if (pct >= 90) return 'bg-red-500'
  if (pct >= 70) return 'bg-amber-500'
  return 'bg-green-600'
}

const num = (n: number) => n.toLocaleString()

const BucketCard = (bucket: TBucketResponse) => {
  const navigate = useNavigate()
  // s3 mode lists one region at a time: the one these credentials signed in to.
  const region = useS3Credentials(state => state.region)

  const storageLimited = bucket.quota_enabled && bucket.hard_bytes_raw > 0
  const pct = storageLimited
    ? Math.min(
        100,
        Math.round(calculateValue(bucket.used_bytes_raw, bucket.hard_bytes_raw))
      )
    : 0
  const objectsLimited = bucket.quota_enabled && bucket.hard_objects > 0

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
          <div className="flex items-center gap-1">
            <TooltipProvider>
              <Tooltip>
                <TooltipTrigger asChild>
                  <h3 className="truncate text-lg font-semibold leading-tight">
                    {bucket.bucket}
                  </h3>
                </TooltipTrigger>
                <TooltipContent>
                  <p>{bucket.bucket}</p>
                </TooltipContent>
              </Tooltip>
            </TooltipProvider>
            <CopyButton name={bucket.bucket} />
          </div>
          <div className="mt-1 flex min-w-0 items-center gap-2">
            <RegionBadge region={region} />
            <p className="truncate text-xs text-muted-foreground">
              {bucket.access} · {t('modified')}{' '}
              {timeAgo(bucket.modify_time_stamp)}
            </p>
          </div>
        </div>
        <BucketMenu bucket={bucket.bucket} />
      </div>

      <div className="flex flex-col gap-1.5">
        <div className="flex items-center justify-between text-sm">
          <span className="font-medium">{t('storage')}</span>
          <span className="text-muted-foreground">
            {storageLimited
              ? `${pct}% · ${bucket.used_bytes} ${bucket.used_bytes_unit} / ${bucket.hard_bytes} ${bucket.hard_bytes_unit}`
              : `${bucket.used_bytes} ${bucket.used_bytes_unit}`}
          </span>
        </div>
        {storageLimited ? (
          <Progress
            value={pct}
            className="h-2"
            indicatorClassName={usageColor(pct)}
          />
        ) : null}
      </div>

      <div className="flex items-center justify-between text-sm">
        <span className="font-medium">{t('objects')}</span>
        <span className="text-muted-foreground">
          {objectsLimited
            ? `${num(bucket.used_objects)} / ${num(bucket.hard_objects)}`
            : num(bucket.used_objects)}
        </span>
      </div>

      <Button
        className="mt-auto w-full"
        data-test="browse-bucket-button"
        onClick={() =>
          navigate({
            to: `/object-storage/s3-bucket/buckets/${bucket.bucket}`
          })
        }
      >
        {t('browse')}
      </Button>
    </div>
  )
}

export default BucketCard

const CopyButton = ({ name }: { name: string }) => {
  const { toast } = useToast()

  return (
    <Button
      variant="link"
      size="icon"
      className="size-6 shrink-0 text-muted-foreground hover:text-foreground"
      onClick={() =>
        copyString(name).then(key =>
          toast({ title: `${key} copied`, variant: 'info' })
        )
      }
    >
      <Copy className="size-4" />
    </Button>
  )
}

const BucketMenu = ({ bucket }: { bucket: string }) => {
  const [openDeleteConfirmation, setOpenDeleteConfirmation] = useState(false)
  const [openMenu, setOpenMenu] = useState(false)
  const { mutate: deleteBucket, isPending } = useDeleteBucket()

  return (
    <div data-test="bucket-more-details">
      <DropdownMenu
        open={openMenu && !openDeleteConfirmation}
        onOpenChange={setOpenMenu}
      >
        <DropdownMenuTrigger asChild>
          <Button
            variant="ghost"
            size="icon"
            className="size-8 shrink-0"
            data-test="bucket-more-details-trigger"
          >
            <Ellipsis className="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem onSelect={e => e.preventDefault()} className="p-0">
            <DeleteBucket
              disable={isPending}
              clickHandler={() => {
                setOpenDeleteConfirmation(true)
                setOpenMenu(false)
              }}
            />
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <DeleteConfirmation
        open={openDeleteConfirmation}
        closeHandler={() => setOpenDeleteConfirmation(false)}
        acceptDelete={() => deleteBucket(bucket)}
        deleteItemName={bucket}
      />
    </div>
  )
}
