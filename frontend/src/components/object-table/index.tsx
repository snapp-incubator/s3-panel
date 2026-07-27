import { AlertCircle, Folder, Share2 } from 'lucide-react'

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
  isError,
  isLoading,
  bucket,
  currentPath,
  objectList,
  isSearch,
  refetchObjects,
  onShareObject,
  onNavigateToFolder
}: TObjectTablesProps) {
  return (
    <div>
      {isError || (!isLoading && !objectList?.items) ? (
        <div className="p-4">
          <Alert>
            <AlertCircle className="size-5 !text-blue-400" />
            <AlertTitle className="!text-blue-400">{t('no_data')}</AlertTitle>
            {isSearch ? null : (
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
                        <div className="flex items-center gap-2">
                          <Folder className="size-4 text-blue-500" />
                          {displayName}
                        </div>
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
                        <Button
                          size="icon"
                          variant="ghost"
                          onClick={() => onShareObject(objectKey)}
                        >
                          <Share2 />
                        </Button>
                        <DeleteObject
                          object={objectKey}
                          bucket={bucket}
                          refetchObjects={refetchObjects}
                        />
                        <DownloadObject bucket={bucket} object={objectKey} />
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
