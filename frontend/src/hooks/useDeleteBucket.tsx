import { useMutation } from '@tanstack/react-query'

import { deleteBucketApi } from '@/api/s3'
import { bucketsKeys } from '@/api/s3Keys'
import { t } from '@/i18n'
import { errorToastContent } from '@/lib/requestError'
import { queryClient } from '@/services/http/query-client'

import type { HTTPClientError } from '../services/http/interceptorsConfig'

import { useToast } from './use-toast'

const useDeleteBucket = () => {
  const { toast } = useToast()

  return useMutation({
    mutationFn: async (bucket: string) => deleteBucketApi(bucket),
    onSuccess: () => {
      toast({
        variant: 'success',
        title: t('success_delete_bucket')
      })
      queryClient.refetchQueries({
        queryKey: bucketsKeys.all()
      })
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      toast({
        variant: 'destructive',
        ...errorToastContent(err, t('failed_delete_bucket'))
      })
    }
  })
}

export default useDeleteBucket
