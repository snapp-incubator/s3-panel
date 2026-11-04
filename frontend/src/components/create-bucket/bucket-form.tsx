import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
import { Button } from '@/components/shadcn/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage
} from '@/components/shadcn/form'
import { Textarea } from '@/components/shadcn/textarea'
import { t } from '@/i18n'
import type { z } from '@/lib/zod'

import { FormSchemaType } from './createBucket.types'

interface BucketFormProps {
  onSubmit: (values: z.infer<typeof FormSchemaType>) => void
  onClose: () => void
  isPending: boolean
}

const BucketForm = ({ onSubmit, onClose, isPending }: BucketFormProps) => {
  const form = useForm<z.infer<typeof FormSchemaType>>({
    resolver: zodResolver(FormSchemaType),
    defaultValues: {
      bucket: ''
    }
  })

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)}>
        <FormField
          control={form.control}
          name="bucket"
          render={({ field }) => (
            <FormItem className="flex flex-col gap-2">
              <FormLabel>{t('insert_name')}</FormLabel>
              <FormControl>
                <Textarea
                  placeholder={t('bucket_name_placeholder')}
                  {...field}
                  data-test="bucket-name-input"
                />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="mt-5 flex justify-end gap-2">
          <Button type="submit" className="bg-green-700" disabled={isPending}>
            {isPending ? t('creating') : t('create')}
          </Button>
          <Button type="button" variant="secondary" onClick={onClose}>
            {t('discard')}
          </Button>
        </div>
      </form>
    </Form>
  )
}

export default BucketForm
