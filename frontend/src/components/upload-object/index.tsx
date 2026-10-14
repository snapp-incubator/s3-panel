import FileUpload from '@/components/file-upload'
import useFileUploadMutation from '@/hooks/useFileUploadMutation'
import useFileUploadQueue from '@/hooks/useFileUploadQueue'
import type { IUploadNames } from '@/types/s3/upload.types'

import { useUploadProgress } from '../providers/uploadProgressContext'

import type { TUploadObjectProps } from './uploadObject.types'

export default function UploadObject({
  bucketName,
  region,
  currentPath,
  refetchObjects
}: TUploadObjectProps) {
  const { setUploadNames } = useUploadProgress()

  const { mutateAsync } = useFileUploadMutation({
    bucketName,
    region,
    currentPath,
    refetchObjects
  })

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const files = event.target.files

    if (files?.length) {
      const filesStorage = Array.from(files)

      const initialNames: IUploadNames[] = filesStorage.map(file => ({
        name: file.name,
        completed: false,
        failed: false,
        progress: 0,
        canceled: false,
        file
      }))

      setUploadNames(prev => [...prev, ...initialNames])
    }
  }

  useFileUploadQueue({ mutateAsync })

  return <FileUpload handleFileChange={handleFileChange} />
}
