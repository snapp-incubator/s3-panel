import { Upload } from 'lucide-react'
import { useRef } from 'react'

import { Button } from '@/components/shadcn/button'
import { t } from '@/i18n'

import type { TFileUploadProps } from './fileUpload.types'

export default function FileUpload({ handleFileChange }: TFileUploadProps) {
  const hiddenFileInput = useRef<HTMLInputElement>(null)

  const handleClick = () => {
    hiddenFileInput.current?.click()
  }

  return (
    <Button
      size="icon"
      // disabled={isLoading}
      variant="ghost"
      onClick={handleClick}
      aria-label={t('upload_objects')}
      title={t('upload_objects')}
      data-test="file-upload-button"
    >
      <Upload aria-hidden="true" size="28" className="text-green-500" />
      <input
        type="file"
        tabIndex={-1}
        aria-hidden="true"
        name="objects"
        id="object-upload"
        className="hidden"
        ref={hiddenFileInput}
        onChange={handleFileChange}
        multiple
        data-test="file-upload-input"
      />
    </Button>
  )
}
