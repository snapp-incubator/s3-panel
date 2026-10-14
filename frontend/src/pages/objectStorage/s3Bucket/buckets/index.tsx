import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useCallback, useEffect, useState } from 'react'
import { useEffectOnce } from 'react-use'

import { fetchIAMBuckets } from '@/api/panel'
import { fetchBucketsQuota } from '@/api/s3'
import { bucketsKeys } from '@/api/s3Keys'
import ActAsBar from '@/components/act-as-bar'
import { AlertMessage } from '@/components/alert-message'
import BucketCard from '@/components/bucket-card'
import IAMBucketCard from '@/components/bucket-card/iam-bucket-card'
import CreateBucket from '@/components/create-bucket'
import CustomPagination from '@/components/custom-pagination'
import ErrorState from '@/components/error-state'
import { useTitle } from '@/components/providers/titleProvider'
import SearchField from '@/components/search-field'
import { Button } from '@/components/shadcn/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from '@/components/shadcn/select'
import BucketCardSkeleton from '@/components/skeletons/BucketCardSkeleton'
import UserQuota from '@/components/user-quota'
import useLastBucketsSearch from '@/hooks/useLastBucketsSearch'
import usePanelSession from '@/hooks/usePanelSession'
import useUrlBackedInput from '@/hooks/useUrlBackedInput'
import { t } from '@/i18n'

const BUCKETS_ROUTE = '/object-storage/s3-bucket/buckets'

// Radix Select reserves the empty string, so "no filter" needs a value of its own.
const ALL_REGIONS = '__all__'

export default function Buckets() {
  const { setTitle } = useTitle()
  const [openCreate, setOpenCreate] = useState(false)
  const maxItems: number = 10

  // Page, search and region filter are read from the URL, so Back from a bucket
  // returns to the same page of the same list.
  const listSearch = useSearch({ from: BUCKETS_ROUTE })
  const { page = 1, search = '', region } = listSearch
  const navigate = useNavigate({ from: BUCKETS_ROUTE })

  const remember = useLastBucketsSearch(state => state.remember)
  useEffect(() => remember(listSearch), [listSearch, remember])

  const commitSearch = useCallback(
    (value: string) =>
      navigate({
        search: prev => ({
          ...prev,
          search: value || undefined,
          page: undefined
        }),
        replace: true
      }),
    [navigate]
  )
  const [searchText, setSearchText] = useUrlBackedInput(search, commitSearch)

  const setPage = (value: number) =>
    navigate({
      search: prev => ({ ...prev, page: value > 1 ? value : undefined })
    })

  const setRegion = (value: string) =>
    navigate({
      search: prev => ({
        ...prev,
        region: value === ALL_REGIONS ? undefined : value,
        page: undefined
      })
    })

  const isIAMMode = usePanelSession(state => state.authMode === 'iam')
  const isAdmin = usePanelSession(state => state.session?.is_admin === true)
  // The server refuses mutating calls outright; hiding the controls keeps the
  // UI honest rather than offering buttons that 403.
  const isReadOnly = usePanelSession(state => state.readOnly)

  // The two modes list different things: s3 lists what the caller's own
  // credentials own (and can report quota inline), iam lists what an
  // authorization service says they may reach, across every region at once.
  const {
    data: buckets,
    isFetching,
    isError,
    refetch
  } = useQuery({
    queryFn: () => fetchBucketsQuota(maxItems, page, search),
    queryKey: bucketsKeys.all(maxItems, page, search),
    enabled: !isIAMMode,
    // Keeps the page count steady while the next page loads.
    placeholderData: keepPreviousData
  })

  const {
    data: iamBuckets,
    isFetching: iamFetching,
    isError: iamError,
    refetch: iamRefetch
  } = useQuery({
    queryFn: () => fetchIAMBuckets(maxItems, page, search, region),
    queryKey: ['iam-buckets', maxItems, page, search, region ?? ''],
    enabled: isIAMMode,
    // Keeps the page count and the region choices steady while loading.
    placeholderData: keepPreviousData
  })

  useEffectOnce(() => {
    setTitle('SnappCloud - s3 Buckets')
  })

  const totalPages =
    (isIAMMode ? iamBuckets?.total_pages : buckets?.total_pages) ?? 0

  // Offered once there is more than one region to choose from, and kept while a
  // filter is applied so it can be cleared.
  const regions = iamBuckets?.regions ?? []
  const showRegionFilter = isIAMMode && (regions.length > 1 || !!region)

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

      {isReadOnly ? (
        <div
          data-test="read-only-banner"
          className="mt-4 rounded-lg border border-sky-500/30 bg-sky-500/10 px-4 py-2 text-sm"
        >
          <span className="font-medium">{t('read_only_panel')}</span>
          <span className="ml-2 text-muted-foreground">
            {t('read_only_panel_hint')}
          </span>
        </div>
      ) : null}

      {isIAMMode ? <ActAsBar /> : null}

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
      <div className="mt-4 flex items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-3">
          <SearchField value={searchText} onChange={setSearchText} />
          {showRegionFilter ? (
            <Select value={region ?? ALL_REGIONS} onValueChange={setRegion}>
              <SelectTrigger
                className="w-[180px]"
                aria-label={t('filter_by_region')}
                data-test="region-filter"
              >
                <SelectValue placeholder={t('all_regions')} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL_REGIONS}>{t('all_regions')}</SelectItem>
                {(region && !regions.includes(region)
                  ? [...regions, region]
                  : regions
                ).map(name => (
                  <SelectItem value={name} key={name}>
                    {name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : null}
        </div>
        {/* A minted credential belongs to no tenant, so a bucket created with it
            would land outside the team's namespace. */}
        {isIAMMode || isReadOnly ? null : (
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
            {totalPages > 1 ? (
              <CustomPagination
                currentPage={page}
                totalPages={totalPages}
                onPageChange={setPage}
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
