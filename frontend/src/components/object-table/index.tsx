import { AlertCircle, Folder, Share2 } from 'lucide-react'

import ErrorState from '@/components/error-state'
import { Alert, AlertDescription, AlertTitle } from '@/components/shadcn/alert'
import { Button } from '@/components/shadcn/button'
import { Skeleton } from '@/components/shadcn/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from '@/components/shadcn/table'
import { t } from '@/i18n'
import { dateFormat } from '@/lib/utils'

import DeleteObject from './delete-object'
import DownloadObject from './download-object'
import type { TObjectTablesProps } from './objectTables.types'

const OBJECT_SKELETON_ROWS = Array.from(
  { length: 6 },
  (_, index) => `object-skeleton_${index}`
)

const ObjectSkeleton = () => {
  return OBJECT_SKELETON_ROWS.map(id => {
    return (
      <TableRow key={id}>
        <TableCell>
          <Skeleton className="h-4 w-full" />
        </TableCell>
        <TableCell>
          <Skeleton className="h-4 w-full" />
        </TableCell>
        <TableCell>
          <Skeleton className="h-4 w-full" />
        </TableCell>
        <TableCell>
          <Skeleton className="h-4 w-full" />
        </TableCell>
      </TableRow>
    )
  })
}

export default function ObjectTable({
  error,
  onRetry,
  isLoading,
  bucket,
  region,
  currentPath,
  objectList,
  isSearch,
  canWrite,
  canShare,
  refetchObjects,
  onShareObject,
  onNavigateToFolder
}: TObjectTablesProps) {
  // A failed listing is not an empty bucket: it says why, and whom to ask.
  if (error) {
    return <ErrorState error={error} subject="bucket" onRetry={onRetry} />
  }

  return (
    <div>
      {!isLoading && !objectList?.items?.length ? (
        <div className="p-4">
          <Alert>
            <AlertCircle className="size-5 !text-blue-400" />
            <AlertTitle className="!text-blue-400">{t('no_data')}</AlertTitle>
            {isSearch || !canWrite ? null : (
              <AlertDescription>{t('upload_first_object')}</AlertDescription>
            )}
          </Alert>
        </div>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('name')}</TableHead>
              <TableHead className="text-center">
                {t('modification_date')}
              </TableHead>
              <TableHead className="text-center">{t('size')}</TableHead>
              <TableHead className="text-center">{t('actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <ObjectSkeleton />
            ) : (
              objectList?.items.map(item => {
                const displayName = item.name.replace(/\/$/, '')
                const objectKey = isSearch ? item.name : currentPath + item.name

                if (item.is_folder) {
                  return (
                    <TableRow
                      key={item.name}
                      className="cursor-pointer hover:bg-muted/50"
                      onClick={() => onNavigateToFolder(item.name)}
                    >
                      <TableCell className="font-medium">
                        {/* The row is clickable for a mouse; the button makes
                            the folder reachable from the keyboard too. */}
                        <button
                          type="button"
                          className="flex items-center gap-2 text-left"
                          onClick={event => {
                            event.stopPropagation()
                            onNavigateToFolder(item.name)
                          }}
                        >
                          <Folder
                            aria-hidden="true"
                            className="size-4 text-blue-500"
                          />
                          {displayName}
                        </button>
                      </TableCell>
                      <TableCell className="text-center text-muted-foreground">
                        —
                      </TableCell>
                      <TableCell className="text-center text-muted-foreground">
                        —
                      </TableCell>
                      <TableCell />
                    </TableRow>
                  )
                }

                return (
                  <TableRow key={item.name}>
                    <TableCell className="font-medium">{item.name}</TableCell>
                    <TableCell className="text-center">
                      {dateFormat(item.last_modified_timestamp)}
                    </TableCell>
                    <TableCell className="text-center">
                      {`${item.size_value} ${item.size_unit}`}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center justify-center gap-2">
                        {/* A share link is refused on a read-only deployment. */}
                        {canShare ? (
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={t('share_object_named', {
                              name: item.name
                            })}
                            title={t('share_object_named', {
                              name: item.name
                            })}
                            onClick={() => onShareObject(objectKey)}
                          >
                            <Share2 aria-hidden="true" />
                          </Button>
                        ) : null}
                        {canWrite ? (
                          <DeleteObject
                            object={objectKey}
                            bucket={bucket}
                            region={region}
                            refetchObjects={refetchObjects}
                          />
                        ) : null}
                        <DownloadObject
                          bucket={bucket}
                          object={objectKey}
                          region={region}
                        />
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      )}
    </div>
  )
}
