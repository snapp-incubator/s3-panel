import { useQuery } from '@tanstack/react-query'
import { Fragment } from 'react'

import { fetchUserQuota } from '@/api/s3'
import { userKeys } from '@/api/s3Keys'
import { Progress } from '@/components/shadcn/progress'
import { Skeleton } from '@/components/shadcn/skeleton'
import { progressItems } from '@/constants/s3/quotaProgressItems'
import { t } from '@/i18n'
import { calculateValue } from '@/lib/utils'

import { Card, CardContent, CardTitle } from '../shadcn/card'

const QUOTA_SKELETON_ITEMS = Array.from(
  { length: 3 },
  (_, index) => `quota-skeleton_${index}`
)

export default function UserQuota() {
  const {
    data: userQuota,
    isFetching,
    isError
  } = useQuery({
    queryFn: fetchUserQuota,
    queryKey: userKeys.allQuota()
  })

  const renderLoadingState = () => (
    <div className="my-6 grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
      {QUOTA_SKELETON_ITEMS.map(id => (
        <Fragment key={id}>
          <Skeleton className="h-2 w-[150px] rounded-lg" />
          <Skeleton className="h-4 w-[250px] rounded-lg" />
        </Fragment>
      ))}
    </div>
  )

  const renderErrorState = () => <></>

  const renderQuotaItems = () => {
    return progressItems.map(item => {
      const hardValue = userQuota?.[item.hardKey] || 0
      const usedValue = userQuota?.[item.usedKey] || 0

      return quotaItem(
        item.title,
        item.key,
        usedValue,
        hardValue,
        calculateValue(usedValue, hardValue)
      )
    })
  }

  const quotaItem = (
    title: string,
    key: string,
    usedValue: string | number,
    hardValue: string | number,
    progressValue: number
  ) => {
    return (
      <div key={key} className="flex flex-col gap-2">
        <span>
          {title}: {`${usedValue} of ${hardValue}`}
        </span>
        <Progress
          value={progressValue}
          className="max-h-3 w-full text-green-700"
        />
      </div>
    )
  }

  if (isFetching) return renderLoadingState()

  if (isError) return renderErrorState()

  if (userQuota) {
    const {
      hard_bytes,
      hard_bytes_raw,
      hard_bytes_unit,
      used_bytes,
      used_bytes_raw,
      used_bytes_unit
    } = userQuota

    return (
      <Card className="my-2 border-none shadow-none">
        <CardTitle className="p-4 px-0 text-xl">{t('user_quota')}</CardTitle>
        <CardContent className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
          {renderQuotaItems()}
          {quotaItem(
            t('bytes_usage'),
            'byte',
            `${used_bytes} ${used_bytes_unit}`,
            `${hard_bytes} ${hard_bytes_unit}`,
            calculateValue(used_bytes_raw, hard_bytes_raw)
          )}
        </CardContent>
      </Card>
    )
  }

  return null
}
