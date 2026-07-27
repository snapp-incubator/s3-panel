import { Skeleton } from '@/components/shadcn/skeleton'

interface IBucketCardSkeletonProps {
  count: number
}

export default function BucketCardSkeleton({
  count
}: IBucketCardSkeletonProps) {
  const skeletonIds = Array.from(
    { length: count },
    (_, index) => `bucket-card-skeleton_${index}`
  )

  return skeletonIds.map(id => (
    <div
      key={id}
      className="flex min-w-[300px] flex-col gap-4 rounded-xl border bg-card p-5"
    >
      <div className="flex items-start gap-3">
        <Skeleton className="size-10 shrink-0 rounded-lg" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-2/3 rounded" />
          <Skeleton className="h-3 w-1/2 rounded" />
        </div>
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-full rounded" />
        <Skeleton className="h-2 w-full rounded-full" />
      </div>
      <Skeleton className="h-4 w-full rounded" />
      <Skeleton className="mt-auto h-9 w-full rounded-md" />
    </div>
  ))
}
