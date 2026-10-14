import { useMutation } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { useState } from 'react'

import { deleteObject } from '@/api/s3'
import DeleteConfirmation from '@/components/delete-confirmation'
import { Button } from '@/components/shadcn/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger
} from '@/components/shadcn/tooltip'
import { useToast } from '@/hooks/use-toast'
import { t } from '@/i18n'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'

import type { TDeleteObjectProps } from './deleteObject.types'

export default function DeleteObject({
  bucket,
  object,
  region,
  refetchObjects
}: TDeleteObjectProps) {
  const [open, setOpen] = useState(false)

  const { toast } = useToast()

  const { mutate, isPending } = useMutation({
    mutationFn: () => deleteObject(bucket, [object], region),
    onSuccess: () => {
      setOpen(false)
      refetchObjects()
      toast({
        variant: 'success',
        title: t('success_delete_object')
      })
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      setOpen(false)
      toast({
        variant: 'destructive',
        title: err.response?.data.message || t('failed_delete_object')
      })
    }
  })

  const deleteHandler = () => {
    setOpen(true)
  }

  return (
    <>
      <TooltipProvider delayDuration={50}>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button size="icon" variant="ghost" onClick={deleteHandler}>
              <Trash2 />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('delete_object')}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>

      <DeleteConfirmation
        open={open}
        isLoading={isPending}
        closeHandler={() => setOpen(false)}
        deleteItemName={object}
        acceptDelete={() => mutate()}
      />
    </>
  )
}
