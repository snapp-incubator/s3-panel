import { Globe } from 'lucide-react'

import { t } from '@/i18n'

/** The region a bucket lives in, as a tag. */
const RegionBadge = ({ region }: { region?: string | null }) => {
  if (!region) return null

  return (
    <span
      data-test="bucket-region"
      title={t('region')}
      className="inline-flex shrink-0 items-center gap-1 rounded-md border bg-muted/50 px-1.5 py-0.5 text-xs font-medium text-muted-foreground"
    >
      <Globe className="size-3" />
      {region}
    </span>
  )
}

export default RegionBadge
