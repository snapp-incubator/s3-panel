import { ChevronDown, ChevronUp, X } from 'lucide-react'
import { useState } from 'react'

import { Progress } from '@/components/shadcn/progress'
import { t } from '@/i18n'
import type { IUploadNames } from '@/types/s3/upload.types'

import { useUploadProgress } from '../providers/uploadProgressContext'
import { Button } from '../shadcn/button'

const GlobalUploadProgress = () => {
  const {
    uploadNames,
    abortController,
    updateUploadNamesState,
    showUploadModal,
    setShowUploadModal
  } = useUploadProgress()

  const [showModalContent, setShowModalContent] = useState(true)

  const handleCancel = (uploadItem: IUploadNames) => {
    if (uploadItem.progress && uploadItem.progress > 0) {
      if (abortController.current) {
        abortController.current.abort()
        abortController.current = null
      }
    }

    // Mark the upload as canceled, which prevents it from uploading and hides it from the list.
    updateUploadNamesState(uploadItem.name, { canceled: true })
  }

  const handleRetry = (name: string) => {
    updateUploadNamesState(name, {
      failed: false,
      completed: false,
      canceled: false,
      progress: 0
    })

    const event = new CustomEvent('retry-upload', { detail: { name } })

    window.dispatchEvent(event)
  }

  const returnProgress = (item: IUploadNames) => {
    if (item.completed) {
      return 100
    }

    return item.progress
  }

  if (!showUploadModal) return null

  return (
    <div className="fixed bottom-0 right-0 z-50 w-full max-w-md bg-background shadow-lg">
      <div className="flex items-center justify-center rounded-t-xl bg-[#1a1c20] px-4 py-2 text-white">
        <h4 className="mb-2 mr-auto font-semibold">{t('upload')}</h4>
        <Button variant="link" size="icon">
          {showModalContent ? (
            <ChevronDown
              className="size-6 text-white"
              onClick={() => setShowModalContent(false)}
            />
          ) : (
            <ChevronUp
              className="size-6 text-white"
              onClick={() => setShowModalContent(true)}
            />
          )}
        </Button>
        <Button
          variant="link"
          size="icon"
          onClick={() => setShowUploadModal(false)}
        >
          <X className="size-6 text-white" />
        </Button>
      </div>
      {showModalContent
        ? uploadNames.map(item =>
            item.canceled ? null : (
              <div key={item.name} className="mb-4 p-4">
                <div className="mb-1 flex items-center justify-between">
                  <span className="w-1/2 truncate">{item.name}</span>
                  <div className="flex items-center gap-2">
                    {item.failed ? null : <span>{returnProgress(item)}%</span>}
                    {!item.completed && !item.failed && (
                      <button
                        type="button"
                        onClick={() => handleCancel(item)}
                        className="rounded bg-red-500 px-2 py-1 text-xs text-white"
                      >
                        {t('cancel')}
                      </button>
                    )}
                    {item.failed && (
                      <button
                        type="button"
                        onClick={() => handleRetry(item.name)}
                        className="rounded bg-blue-500 px-2 py-1 text-xs text-white"
                      >
                        {t('retry')}
                      </button>
                    )}
                  </div>
                </div>
                {item.failed ? (
                  <span className="text-red-500">{t('failed')}</span>
                ) : (
                  <Progress value={returnProgress(item)} />
                )}
              </div>
            )
          )
        : null}
    </div>
  )
}

export default GlobalUploadProgress
