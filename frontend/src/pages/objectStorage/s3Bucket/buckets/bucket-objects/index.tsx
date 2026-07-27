import { useQuery } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { useDeferredValue, useState } from 'react'

import { fetchObjects } from '@/api/s3'
import { bucketObjectKeys } from '@/api/s3Keys'
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

export default function BucketObjects() {
  const { setTitle } = useTitle()

  const { bucketName } = useParams({ strict: false })

  const [currentPage, setCurrentPage] = useState(1)
  const [currentPath, setCurrentPath] = useState('')

  useEffectOnce(() => {
    setTitle('SnappCloud - S3 Bucket Objects')
  })

  const navigate = useNavigate()

  const [openShareObject, setOpenShareObject] = useState(false)
  const [objectName, setObjectName] = useState<string | null>(null)

  const [searchValue, setSearchValue] = useState('')
  const deferredSearch = useDeferredValue(searchValue)

  const {
    data: objectList,
    isLoading,
    refetch,
    isError
  } = useQuery({
    queryKey: bucketObjectKeys.all(
      bucketName!,
      currentPage,
      deferredSearch,
      currentPath
    ),
    queryFn: () =>
      fetchObjects(bucketName!, currentPage, deferredSearch, currentPath)
  })

  const navigateToFolder = (folderName: string) => {
    setCurrentPath(prev => prev + folderName)
    setCurrentPage(1)
    setSearchValue('')
  }

  const navigateToPath = (path: string) => {
    setCurrentPath(path)
    setCurrentPage(1)
  }

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
      <h2 className="text-3xl">S3 Bucket</h2>
      <Breadcrumb className="mt-4">
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink>
              <Button
                size="sm"
                variant="link"
                onClick={() =>
                  navigate({
                    to: '/object-storage/s3-bucket/buckets'
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
        <SearchField
          value={searchValue}
          onChange={value => {
            setSearchValue(value)
            setCurrentPage(1)
          }}
        />
        <UploadObject
          bucketName={bucketName!}
          currentPath={currentPath}
          refetchObjects={() => refetch()}
        />
      </div>
      <Card className="mt-8">
        <CardContent className="p-0">
          <ObjectTable
            isError={isError}
            isLoading={isLoading}
            objectList={objectList!}
            bucket={bucketName!}
            currentPath={currentPath}
            refetchObjects={() => refetch()}
            isSearch={!!searchValue}
            onShareObject={objectName => {
              setObjectName(objectName)
              setOpenShareObject(true)
            }}
            onNavigateToFolder={navigateToFolder}
          />
        </CardContent>
        <CardFooter className="relative">
          {objectList?.total_pages && objectList.total_pages > 1 ? (
            <CustomPagination
              currentPage={currentPage}
              totalPages={objectList?.total_pages}
              onPageChange={value => setCurrentPage(value)}
            />
          ) : null}
        </CardFooter>
      </Card>
      <ShareObject
        open={openShareObject}
        bucket={bucketName!}
        object={objectName!}
        closeHandler={() => setOpenShareObject(false)}
      />
    </div>
  )
}
