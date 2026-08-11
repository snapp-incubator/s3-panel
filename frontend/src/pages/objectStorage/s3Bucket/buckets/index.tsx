import { useQuery } from '@tanstack/react-query'
import { useDeferredValue, useEffect, useState } from 'react'
import { useEffectOnce } from 'react-use'

import { fetchIAMBuckets } from '@/api/panel'
import { fetchBucketsQuota } from '@/api/s3'
import { bucketsKeys } from '@/api/s3Keys'
import { AlertMessage } from '@/components/alert-message'
import BucketCard from '@/components/bucket-card'
import IAMBucketCard from '@/components/bucket-card/iam-bucket-card'
import CreateBucket from '@/components/create-bucket'
import CustomPagination from '@/components/custom-pagination'
import ErrorState from '@/components/error-state'
import { useTitle } from '@/components/providers/titleProvider'
import SearchField from '@/components/search-field'
import { Button } from '@/components/shadcn/button'
import BucketCardSkeleton from '@/components/skeletons/BucketCardSkeleton'
import UserQuota from '@/components/user-quota'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'

export default function Buckets() {
  const { setTitle } = useTitle()
  const [openCreate, setOpenCreate] = useState(false)
  const [page, setPage] = useState(1)
  const [initialTotalPages, setInitialTotalPages] = useState<number | null>(
    null
  )
  const [searchValue, setSearchValue] = useState('')
  const deferredSearch = useDeferredValue(searchValue)
  const maxItems: number = 10

  const isIAMMode = usePanelSession(state => state.authMode === 'iam')
  const isAdmin = usePanelSession(state => state.session?.is_admin === true)

  // The two modes list different things: s3 lists what the caller's own
  // credentials own (and can report quota inline), iam lists what an
  // authorization service says they may reach, across every region at once.
  const {
    data: buckets,
    isFetching,
    isError,
    refetch
  } = useQuery({
    queryFn: () => fetchBucketsQuota(maxItems, page, deferredSearch),
    queryKey: bucketsKeys.all(maxItems, page, deferredSearch),
    enabled: !isIAMMode
  })

  const {
    data: iamBuckets,
    isFetching: iamFetching,
    isError: iamError,
    refetch: iamRefetch
  } = useQuery({
    queryFn: () => fetchIAMBuckets(maxItems, page, deferredSearch),
    queryKey: ['iam-buckets', maxItems, page, deferredSearch],
    enabled: isIAMMode
  })

  useEffectOnce(() => {
    setTitle('SnappCloud - s3 Buckets')
  })

  useEffect(() => {
    const totalPages = isIAMMode
      ? iamBuckets?.total_pages
      : buckets?.total_pages

    if (totalPages !== undefined && initialTotalPages === null) {
      setInitialTotalPages(totalPages)
    }
  }, [buckets, iamBuckets, isIAMMode, initialTotalPages])

  const returnBuckets = () => {
    if (isIAMMode) {
      if (iamBuckets?.items?.length) {
        return iamBuckets.items.map(bucket => (
          <IAMBucketCard
            {...bucket}
            key={`${bucket.region}/${bucket.bucket}`}
          />
        ))
      }

      return (
        <AlertMessage
          title={t('empty_buckets')}
          message={t('no_accessible_buckets')}
        />
      )
    }

    if (buckets?.items) {
      return buckets.items.map(bucket => (
        <BucketCard {...bucket} key={bucket.bucket} />
      ))
    }

    return (
      <AlertMessage
        title={t('empty_buckets')}
        message={t('create_first_bucket')}
      />
    )
  }

  return (
    <div>
      <div className="flex flex-col justify-between gap-4 md:flex-row md:gap-0">
        <h2 className="text-3xl">{t('s3_bucket')}</h2>
      </div>
      {/* Per-user quota is an s3-mode concept: in iam mode a user has no single
          gateway account to carry one, so quota is per bucket on the detail page. */}
      {isIAMMode ? null : <UserQuota />}

      {isIAMMode && isAdmin ? (
        <div
          data-test="admin-view-banner"
          className="mt-4 rounded-lg border border-amber-500/30 bg-amber-500/10 px-4 py-2 text-sm"
        >
          <span className="font-medium">{t('admin_view')}</span>
          <span className="ml-2 text-muted-foreground">
            {t('admin_view_hint')}
          </span>
        </div>
      ) : null}

      <span className="mt-2 block text-xl font-semibold">{t('buckets')}</span>
      <div className="mt-4 flex items-center justify-between">
        <SearchField
          value={searchValue}
          onChange={value => {
            setSearchValue(value)
            setPage(1)
          }}
        />
        {/* A minted credential belongs to no tenant, so a bucket created with it
            would land outside the team's namespace. */}
        {isIAMMode ? null : (
          <Button size="sm" onClick={() => setOpenCreate(true)}>
            {t('create_bucket')}
          </Button>
        )}
      </div>

      {(isIAMMode ? iamError : isError) ? (
        <div className="mt-20">
          <ErrorState />
        </div>
      ) : (
        <>
          <div className="mt-12 grid grid-cols-1 gap-6 lg:grid-cols-2 xl:grid-cols-3">
            {(isIAMMode ? iamFetching : isFetching) ? (
              <BucketCardSkeleton count={6} />
            ) : (
              returnBuckets()
            )}
          </div>
          <div className="relative mt-5">
            {initialTotalPages && initialTotalPages > 1 && !searchValue ? (
              <CustomPagination
                currentPage={page}
                totalPages={initialTotalPages}
                onPageChange={value => setPage(value)}
              />
            ) : null}
          </div>
        </>
      )}
      <CreateBucket
        open={openCreate}
        closeHandler={() => setOpenCreate(false)}
        updateBuckets={isIAMMode ? iamRefetch : refetch}
      />
    </div>
  )
}
