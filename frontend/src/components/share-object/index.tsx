import CopyAddress from '@/components/copy-address'
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
import { expirationItems } from '@/constants/s3/shareLink'
import useCreateShareLink from '@/hooks/useCreateShareLink'
import { t } from '@/i18n'

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
    mutate,
    copyAddress,
    setCopyAddress,
    expirationTime,
    setExpirationTime
  } = useCreateShareLink({
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
          <ToggleGroup type="single">
            {expirationItems.map(item => {
              return (
                <ToggleGroupItem
                  value={item.value}
                  key={item.value}
                  onClick={() => setExpirationTime(item.value)}
                >
                  <span className="text-xs">{item.title}</span>
                </ToggleGroupItem>
              )
            })}
          </ToggleGroup>
          <DialogFooter className="mt-6">
            <div className="flex w-full justify-center gap-4">
              <Button
                className="bg-green-700"
                disabled={!expirationTime}
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
      <CopyAddress
        open={copyAddress}
        title={t('share_address_title')}
        description={t('share_address_description')}
        link={link!}
        closeHandler={() => setCopyAddress(false)}
      />
    </>
  )
}
