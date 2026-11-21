import { useMutation } from '@tanstack/react-query'
import { createBucket } from '@/api/s3'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/shadcn/dialog'
import { useToast } from '@/hooks/use-toast'
import { t } from '@/i18n'
import { errorToastContent } from '@/lib/requestError'
import type { z } from '@/lib/zod'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'

import BucketForm from './bucket-form'
import type { FormSchemaType, ICreateBucket } from './createBucket.types'

export default function CreateBucket({
  open,
  closeHandler,
  updateBuckets
}: ICreateBucket) {
  const { toast } = useToast()

  const { mutate, isPending } = useMutation({
    mutationFn: (values: z.infer<typeof FormSchemaType>) =>
      createBucket({
        bucket: values.bucket
      }),
    onSuccess: () => {
      closeHandler()
      toast({
        variant: 'success',
        title: t('success_create_bucket')
      })
      updateBuckets()
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      toast({
        variant: 'destructive',
        ...errorToastContent(err, t('failed_create_bucket'))
      })
    }
  })

  return (
    <Dialog open={open} onOpenChange={closeHandler}>
      <DialogTrigger />
      <DialogContent className="sm:max-w-md">
        <DialogHeader className="mb-4">
          <DialogTitle>{t('bucket_creation')}</DialogTitle>
        </DialogHeader>
        <BucketForm
          onSubmit={mutate}
          onClose={closeHandler}
          isPending={isPending}
        />
      </DialogContent>
    </Dialog>
  )
}
