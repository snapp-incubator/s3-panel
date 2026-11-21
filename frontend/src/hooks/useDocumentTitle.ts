import { useEffect } from 'react'

import { t } from '@/i18n'

/**
 * Names the browser tab after the page: "<page> · Object Storage". Each part
 * is joined with " · ", so a bucket page reads "my-bucket · Object Storage".
 */
const useDocumentTitle = (...parts: string[]) => {
  const title = [...parts.filter(Boolean), t('app_name')].join(' · ')

  useEffect(() => {
    document.title = title
  }, [title])
}

export default useDocumentTitle
