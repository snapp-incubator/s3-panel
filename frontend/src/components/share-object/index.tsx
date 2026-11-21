import { Button } from '@/components/shadcn/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger
} from '@/components/shadcn/dialog'
import { ToggleGroup, ToggleGroupItem } from '@/components/shadcn/toggle-group'
import { formatShareDuration } from '@/constants/s3/shareLink'
import useCreateShareLink from '@/hooks/useCreateShareLink'
import { t } from '@/i18n'

import ShareCreated from './share-created'

export interface IShareObjectProps {
  open: boolean
  bucket: string
  object: string
  /** The bucket's region; undefined keeps the signed-in region (s3 mode). */
  region?: string
  closeHandler: () => void
}

export default function ShareObject({
  open,
  closeHandler,
  bucket,
  object,
  region
}: IShareObjectProps) {
  const {
    link,
    expiresAt,
    createdSessionBound,
    mutate,
    isPending,
    copyAddress,
    setCopyAddress,
    expirationTime,
    setExpirationTime,
    options,
    limitPending,
    maxSeconds,
    capped,
    sessionBound
  } = useCreateShareLink({
    open,
    bucket,
    object,
    region,
    closeHandler
  })

  return (
    <>
      <Dialog open={open} onOpenChange={closeHandler}>
        <DialogTrigger />
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t('share_address_title')}</DialogTitle>
          </DialogHeader>
          <p className="text-base">{t('share_object_subtitle')}</p>
          <span className="text-base">{`${t('expiration_time')}:`}</span>
          {limitPending ? (
            <p className="text-sm text-muted-foreground">
              {t('share_limit_loading')}
            </p>
          ) : options.length === 0 ? (
            <p data-test="share-no-duration" className="text-sm">
              {t('share_no_duration_available')}
            </p>
          ) : (
            <ToggleGroup
              type="single"
              className="flex-wrap"
              aria-label={t('expiration_time')}
              value={expirationTime ?? ''}
              onValueChange={value => setExpirationTime(value || null)}
            >
              {options.map(item => {
                return (
                  <ToggleGroupItem value={item.value} key={item.value}>
                    <span className="text-xs">{item.title}</span>
                  </ToggleGroupItem>
                )
              })}
            </ToggleGroup>
          )}
          {!limitPending && capped && options.length > 0 ? (
            <p
              data-test="share-expiry-capped"
              className="text-sm text-muted-foreground"
            >
              {t('share_expiry_capped', {
                duration: formatShareDuration(maxSeconds)
              })}
            </p>
          ) : null}
          {sessionBound ? (
            <p
              data-test="share-session-warning"
              className="text-sm text-muted-foreground"
            >
              {t('share_session_bound_warning')}
            </p>
          ) : null}
          <DialogFooter className="mt-6">
            <div className="flex w-full justify-center gap-4">
              <Button
                className="bg-green-700"
                disabled={!expirationTime || isPending || limitPending}
                onClick={() => mutate()}
              >
                {t('create_link')}
              </Button>
              <DialogClose asChild>
                <Button type="button" variant="secondary">
                  {t('discard')}
                </Button>
              </DialogClose>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ShareCreated
        open={copyAddress}
        link={link ?? ''}
        expiresAt={expiresAt}
        sessionBound={createdSessionBound}
        closeHandler={() => setCopyAddress(false)}
      />
    </>
  )
}
