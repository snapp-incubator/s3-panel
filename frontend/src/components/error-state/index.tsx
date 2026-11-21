import {
  CloudOff,
  ExternalLink,
  RotateCw,
  ShieldX,
  TriangleAlert
} from 'lucide-react'

import { Button } from '@/components/shadcn/button'
import usePanelSession from '@/hooks/usePanelSession'
import { t } from '@/i18n'
import {
  requestErrorKind,
  serverMessage,
  unreachableHint,
  whoToAsk
} from '@/lib/requestError'

interface ErrorStateProps {
  /** What the failed query threw. */
  error: unknown
  /** What could not be shown, for the wording of a refusal. */
  subject: 'bucket' | 'buckets'
  /** Offered for anything but a refusal, which asking again will not change. */
  onRetry?: () => void
}

/**
 * Why a page has nothing to show, and what to do about it.
 *
 * A refusal (403/404) says whom to ask for access; an outage (no answer, 5xx)
 * names what could not be reached and offers a retry. Neither is presented as
 * an empty bucket: that told people to upload into a bucket they could not see.
 */
export default function ErrorState({
  error,
  subject,
  onRetry
}: ErrorStateProps) {
  const isIAMMode = usePanelSession(state => state.authMode === 'iam')
  const iamConsoleUrl = usePanelSession(state => state.iamConsoleUrl)
  const kind = requestErrorKind(error)

  const content = {
    denied: {
      Icon: ShieldX,
      title:
        subject === 'bucket'
          ? t('error_not_found_or_no_access')
          : t('error_no_access'),
      lines: [
        subject === 'bucket'
          ? t('error_bucket_not_found_or_no_access')
          : t('error_buckets_no_access'),
        whoToAsk(isIAMMode)
      ]
    },
    unavailable: {
      Icon: CloudOff,
      title: isIAMMode
        ? t('error_cant_reach_iam')
        : t('error_cant_reach_storage'),
      lines: [unreachableHint(isIAMMode)]
    },
    failed: {
      Icon: TriangleAlert,
      title: t('error_request_failed'),
      lines: [serverMessage(error) ?? t('error_request_failed_hint')]
    }
  }[kind]

  return (
    <div
      role="alert"
      data-test="error-state"
      data-test-kind={kind}
      className="mx-auto flex max-w-xl flex-col items-center gap-3 px-4 py-10 text-center"
    >
      <content.Icon aria-hidden="true" className="size-8 text-red-500" />
      <h2 className="text-lg font-medium">{content.title}</h2>
      {content.lines.map(line => (
        <p key={line} className="text-sm text-muted-foreground">
          {line}
        </p>
      ))}

      {kind === 'denied' && isIAMMode && iamConsoleUrl ? (
        <Button asChild variant="outline" size="sm">
          <a
            href={iamConsoleUrl}
            target="_blank"
            rel="noopener noreferrer"
            data-test="error-state-iam-link"
          >
            <ExternalLink aria-hidden="true" className="mr-2 size-4" />
            {t('open_iam')}
          </a>
        </Button>
      ) : null}

      {kind !== 'denied' && onRetry ? (
        <Button
          variant="outline"
          size="sm"
          onClick={onRetry}
          data-test="error-state-retry"
        >
          <RotateCw aria-hidden="true" className="mr-2 size-4" />
          {t('retry')}
        </Button>
      ) : null}
    </div>
  )
}
