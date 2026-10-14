import { Download } from 'lucide-react'

import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from '@/components/shadcn/tooltip'
import useDownloadObject from '@/hooks/useDownloadObject'
import { t } from '@/i18n'

import { Button } from '../shadcn/button'

import type { TDownloadObjectProps } from './downloadObject.types'

export default function DownloadObject({
  bucket,
  object,
  region
}: TDownloadObjectProps) {
  const { mutate: downloadObject } = useDownloadObject(bucket, object, region)

  return (
    <TooltipProvider delayDuration={50}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button size="icon" variant="ghost" onClick={() => downloadObject()}>
            <Download />
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          <p>{t('download_object')}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
