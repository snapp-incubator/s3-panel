import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { Loader2 } from 'lucide-react'
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { fetchBucketsList, fetchRegions } from '@/api/s3'
import SelectField from '@/components/select-field'
import { Button } from '@/components/shadcn/button'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage
} from '@/components/shadcn/form'
import { Input } from '@/components/shadcn/input'
import { formItems } from '@/constants/s3/s3LoginForm'
import { useToast } from '@/hooks/use-toast'
import useS3Credentials from '@/hooks/useS3Credentials'
import { t } from '@/i18n'
import { z } from '@/lib/zod'
import { updateRegion } from '@/services/http/centralClient'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'
import { ProjectRegion } from '@/types/enums'
import type { TRegions } from '@/types/regions.types'

const formSchema = z.object({
  region: z.string({
    message: t('region_required')
  }),
  access_key: z.string().min(1, {
    message: t('access_key_required')
  }),
  secret_key: z.string().min(1, {
    message: t('secret_key_required')
  })
})

// "teh-1" -> "Teh 1"
const formatRegion = (region: string) =>
  region
    .split('-')
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ')

export default function S3LoginForm() {
  const navigate = useNavigate()
  const { toast } = useToast()

  const { fillCredentials } = useS3Credentials()

  const form = useForm<z.infer<typeof formSchema>>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      region: ProjectRegion.Teh1,
      access_key: undefined,
      secret_key: undefined
    }
  })

  // Regions are advertised by the backend; fall back to the known ones if the
  // /regions endpoint isn't available (older backend).
  const { data: regionsData } = useQuery({
    queryKey: ['regions'],
    queryFn: fetchRegions,
    staleTime: Number.POSITIVE_INFINITY,
    retry: false
  })

  const regionNames = regionsData?.regions?.length
    ? regionsData.regions
    : [ProjectRegion.Teh1, ProjectRegion.Teh2]
  const regionOptions = regionNames.map(region => ({
    title: formatRegion(region),
    value: region
  }))
  const singleRegion = regionOptions.length === 1

  // Default the selection to the backend's current region (or the only one).
  const defaultRegion =
    regionsData?.current ?? (singleRegion ? regionNames[0] : undefined)
  useEffect(() => {
    if (defaultRegion) form.setValue('region', defaultRegion)
  }, [defaultRegion, form])

  const { mutateAsync, isPending } = useMutation({
    mutationFn: async () => {
      const values = form.getValues()
      const { access_key, secret_key, region } = values

      // Set the credentials as separate headers
      updateRegion(region, {
        access_key,
        secret_key
      })

      return await fetchBucketsList()
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      toast({
        variant: 'destructive',
        title: err.response?.data.message || err.message
      })
    }
  })

  async function onSubmit(values: z.infer<typeof formSchema>) {
    const { secret_key, access_key, region } = values

    try {
      await mutateAsync()

      navigate({
        to: '/object-storage/s3-bucket/buckets'
      })

      fillCredentials(
        {
          secret_key,
          access_key
        },
        region as TRegions
      )
    } catch (err) {
      console.log(err)
    }
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} className="space-y-4">
        {formItems.map(item => {
          // Hide the region selector entirely when there is only one region.
          if (item.key === 'region' && singleRegion) return null

          const selectItems =
            item.key === 'region' ? regionOptions : item.selectItems

          return (
            <FormField
              control={form.control}
              name={item.key}
              key={item.key}
              render={({ field }) => (
                <FormItem className="flex items-baseline justify-between">
                  <FormLabel className="w-[200px]">{item.label}</FormLabel>
                  <div className="w-full">
                    <FormControl>
                      {item.type === 'select' ? (
                        <SelectField
                          items={selectItems!}
                          placeholder={item.placeholder}
                          value={field.value}
                          onChange={field.onChange}
                        />
                      ) : (
                        <Input
                          placeholder={item.placeholder}
                          {...field}
                          autoComplete="off"
                          data-test={`${item.key}_input`}
                        />
                      )}
                    </FormControl>
                    <FormMessage className="mt-3" />
                  </div>
                </FormItem>
              )}
            />
          )
        })}
        <div className="flex flex-row-reverse">
          <Button type="submit" disabled={isPending}>
            {isPending ? <Loader2 className="animate-spin" /> : t('submit')}
          </Button>
        </div>
      </form>
    </Form>
  )
}
