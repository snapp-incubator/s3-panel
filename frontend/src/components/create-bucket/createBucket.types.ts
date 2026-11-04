import { t } from '@/i18n'
import { z } from '@/lib/zod'

export interface ICreateBucket {
  open: boolean
  closeHandler: () => void
  updateBuckets: () => void
}

export const FormSchemaType = z.object({
  bucket: z.string().min(1, {
    message: t('error_bucket_name')
  })
})
