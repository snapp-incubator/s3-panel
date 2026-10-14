import { useMutation } from '@tanstack/react-query'

import { uploadObjects } from '@/api/s3'
import { useUploadProgress } from '@/components/providers/uploadProgressContext'
import { t } from '@/i18n'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'

import { useToast } from './use-toast'

interface IUseUploadMutationProps {
  bucketName: string
  region?: string
  currentPath: string
  refetchObjects: () => void
}

export default function useFileUploadMutation({
  bucketName,
  region,
  currentPath,
  refetchObjects
}: IUseUploadMutationProps) {
  const { abortController, updateUploadNamesState } = useUploadProgress()
  const { toast } = useToast()
  const toastDuration = 1000

  const { mutateAsync } = useMutation({
    mutationFn: async (file: File) => {
      const formData = new FormData()

      formData.append('files', file)
      formData.append('bucket', bucketName)
      formData.append('prefix', currentPath)

      const controller = new AbortController()

      abortController.current = controller

      return await uploadObjects(
        formData,
        progress => {
          updateUploadNamesState(file.name, { progress })
        },
        region
      )
    },
    onSuccess: () => {
      refetchObjects()
      abortController.current = null
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      // For abort errors, we check the name since fetch abort errors have name 'AbortError'
      if (err.name !== 'AbortError') {
        toast({
          title: err.response?.data.message || t('error_object_upload'),
          variant: 'destructive',
          duration: toastDuration
        })
        abortController.current = null
      }
    }
  })

  return {
    mutateAsync
  }
}
