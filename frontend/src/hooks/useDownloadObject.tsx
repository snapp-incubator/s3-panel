import { useMutation } from '@tanstack/react-query'

import { downloadObject } from '@/api/s3'
import { t } from '@/i18n'
import { errorToastContent } from '@/lib/requestError'
import { downloadUrl } from '@/lib/utils'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'

import { useToast } from './use-toast'

const useDownloadObject = (bucket: string, object: string, region?: string) => {
  const { toast } = useToast()

  return useMutation({
    mutationFn: () => downloadObject(bucket, object, region),
    onSuccess: res => {
      const url = res.url

      downloadUrl(url)
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      toast({
        variant: 'destructive',
        ...errorToastContent(err, t('failed_download_object'))
      })
    }
  })
}

export default useDownloadObject
