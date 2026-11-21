import { t } from '@/i18n'

interface ReadOnlyBannerProps {
  /**
   * `deployment`: the whole panel refuses writes (`server.read_only`).
   * `bucket`: this person may only read this bucket; `hint` says whom to ask.
   */
  scope: 'deployment' | 'bucket'
  hint?: string
}

/** Says why write controls are missing, so their absence is not a mystery. */
export default function ReadOnlyBanner({ scope, hint }: ReadOnlyBannerProps) {
  return (
    <div
      data-test={
        scope === 'deployment' ? 'read-only-banner' : 'read-only-bucket-banner'
      }
      className="mt-4 rounded-lg border border-sky-500/30 bg-sky-500/10 px-4 py-2 text-sm"
    >
      <span className="font-medium">
        {scope === 'deployment' ? t('read_only_panel') : t('read_only_bucket')}
      </span>
      <span className="ml-2 text-muted-foreground">
        {scope === 'deployment'
          ? t('read_only_panel_hint')
          : `${t('read_only_bucket_hint')} ${hint ?? ''}`.trim()}
      </span>
    </div>
  )
}
