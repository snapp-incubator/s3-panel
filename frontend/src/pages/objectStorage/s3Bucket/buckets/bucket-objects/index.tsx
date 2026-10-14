import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useCallback, useEffect, useState } from 'react'

import { fetchObjects, OBJECTS_PAGE_SIZE } from '@/api/s3'
import { bucketObjectKeys } from '@/api/s3Keys'
import RegionBadge from '@/components/bucket-card/region-badge'
import CustomPagination from '@/components/custom-pagination'
import ObjectTable from '@/components/object-table'
import { useTitle } from '@/components/providers/titleProvider'
import SearchField from '@/components/search-field'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator
} from '@/components/shadcn/breadcrumb'
import { Button } from '@/components/shadcn/button'
import { Card, CardContent, CardFooter } from '@/components/shadcn/card'
import ShareObject from '@/components/share-object'
import UploadObject from '@/components/upload-object'
import useEffectOnce from '@/hooks/useEffectOnce'
import useLastBucketsSearch from '@/hooks/useLastBucketsSearch'
import useObjectListCursors, { listingKey } from '@/hooks/useObjectListCursors'
import usePanelSession from '@/hooks/usePanelSession'
import useUrlBackedInput from '@/hooks/useUrlBackedInput'
import { t } from '@/i18n'

const OBJECTS_ROUTE = '/object-storage/s3-bucket/buckets/$bucketName'

export default function BucketObjects() {
  const { setTitle } = useTitle()

  const { bucketName } = useParams({ from: OBJECTS_ROUTE })
  // Folder, page and search are read from the URL, so Back from anywhere returns
  // to the same place in the bucket instead of page 1 of its root.
  const {
    region,
    path: currentPath = '',
    page: currentPage = 1,
    search = ''
  } = useSearch({ from: OBJECTS_ROUTE })
  const navigate = useNavigate({ from: OBJECTS_ROUTE })

  const isIAMMode = usePanelSession(state => state.authMode === 'iam')
  // In iam mode a bucket is its name AND its region — the same name can exist in
  // two — so every call for it carries the region it was opened from. Undefined
  // in s3 mode keeps the region the user signed in to.
  const bucketRegion = isIAMMode ? (region ?? '') : undefined

  const bucketsSearch = useLastBucketsSearch(state => state.search)

  useEffectOnce(() => {
    setTitle('SnappCloud - S3 Bucket Objects')
  })

  const [openShareObject, setOpenShareObject] = useState(false)
  const [objectName, setObjectName] = useState<string | null>(null)

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

  // Pages are reached by continuation token, not by number: page N starts where
  // page N-1 said the listing continues.
  const listing = listingKey(bucketRegion, bucketName, currentPath, search)
  const trail = useObjectListCursors(state => state.trails[listing])
  const recordPage = useObjectListCursors(state => state.recordPage)
  const token = currentPage === 1 ? '' : trail?.[currentPage - 1]

  // A page this session never reached — a link opened elsewhere — has no token
  // to start from, so the listing starts over.
  useEffect(() => {
    if (token === undefined) {
      navigate({
        search: prev => ({ ...prev, page: undefined }),
        replace: true
      })
    }
  }, [token, navigate])

  const {
    data: objectList,
    isLoading,
    refetch,
    isError
  } = useQuery({
    queryKey: bucketObjectKeys.all(
      bucketName,
      currentPage,
      search,
      currentPath,
      token,
      bucketRegion
    ),
    queryFn: () =>
      fetchObjects(bucketName, {
        page: currentPage,
        searchValue: search,
        prefix: currentPath,
        token,
        region: bucketRegion
      }),
    enabled: token !== undefined
  })

  useEffect(() => {
    if (objectList) {
      recordPage(
        listing,
        currentPage,
        objectList.is_truncated ? objectList.next_continuation_token : undefined
      )
    }
  }, [objectList, listing, currentPage, recordPage])

  // Every page whose start is known so far. More appear as the listing is
  // walked; how many there are in all is not known without counting the bucket.
  const knownPages = Math.max(trail?.length ?? 1, currentPage)

  const goToPage = (page: number) =>
    navigate({
      search: prev => ({ ...prev, page: page > 1 ? page : undefined })
    })

  const navigateToPath = (path: string) =>
    navigate({ search: { region, path: path || undefined } })

  const navigateToFolder = (folderName: string) =>
    navigateToPath(currentPath + folderName)

  const pathSegments = currentPath
    ? currentPath
        .split('/')
        .filter(Boolean)
        .map((segment, _index, arr) => {
          const pathUpTo = `${arr.slice(0, _index + 1).join('/')}/`
          return { name: segment, path: pathUpTo }
        })
    : []

  return (
    <div>
      <div className="flex items-center gap-3">
        <h2 className="text-3xl">S3 Bucket</h2>
        <RegionBadge region={region} />
      </div>
      <Breadcrumb className="mt-4">
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink>
              <Button
                size="sm"
                variant="link"
                onClick={() =>
                  navigate({
                    to: '/object-storage/s3-bucket/buckets',
                    search: bucketsSearch
                  })
                }
              >
                Buckets
              </Button>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            {currentPath ? (
              <BreadcrumbLink>
                <Button
                  size="sm"
                  variant="link"
                  onClick={() => navigateToPath('')}
                >
                  {bucketName}
                </Button>
              </BreadcrumbLink>
            ) : (
              <BreadcrumbPage>{bucketName}</BreadcrumbPage>
            )}
          </BreadcrumbItem>
          {pathSegments.map((segment, index) => (
            <span key={segment.path} className="contents">
              <BreadcrumbSeparator />
              <BreadcrumbItem>
                {index === pathSegments.length - 1 ? (
                  <BreadcrumbPage>{segment.name}</BreadcrumbPage>
                ) : (
                  <BreadcrumbLink>
                    <Button
                      size="sm"
                      variant="link"
                      onClick={() => navigateToPath(segment.path)}
                    >
                      {segment.name}
                    </Button>
                  </BreadcrumbLink>
                )}
              </BreadcrumbItem>
            </span>
          ))}
        </BreadcrumbList>
      </Breadcrumb>
      <div className="mt-6 flex items-center justify-between">
        <SearchField value={searchText} onChange={setSearchText} />
        <UploadObject
          bucketName={bucketName}
          region={bucketRegion}
          currentPath={currentPath}
          refetchObjects={() => refetch()}
        />
      </div>
      <Card className="mt-8">
        <CardContent className="p-0">
          <ObjectTable
            isError={isError}
            isLoading={isLoading || token === undefined}
            objectList={objectList!}
            bucket={bucketName}
            region={bucketRegion}
            currentPath={currentPath}
            refetchObjects={() => refetch()}
            isSearch={!!search}
            onShareObject={objectName => {
              setObjectName(objectName)
              setOpenShareObject(true)
            }}
            onNavigateToFolder={navigateToFolder}
          />
        </CardContent>
        <CardFooter className="relative flex-col gap-2">
          {/* A search looks at a bounded part of the bucket per page, so a page
              can come back short while more of the bucket is left to search. */}
          {search &&
          objectList?.is_truncated &&
          objectList.items.length < OBJECTS_PAGE_SIZE ? (
            <p
              data-test="search-partial-hint"
              className="pt-4 text-center text-xs text-muted-foreground"
            >
              {t('search_partial_hint')}
            </p>
          ) : null}
          {knownPages > 1 ? (
            <CustomPagination
              currentPage={currentPage}
              totalPages={knownPages}
              onPageChange={goToPage}
            />
          ) : null}
        </CardFooter>
      </Card>
      <ShareObject
        open={openShareObject}
        bucket={bucketName}
        region={bucketRegion}
        object={objectName!}
        closeHandler={() => setOpenShareObject(false)}
      />
    </div>
  )
}
