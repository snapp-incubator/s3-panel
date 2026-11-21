import { Copy } from 'lucide-react'

import { Button } from '@/components/shadcn/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from '@/components/shadcn/dialog'
import { Input } from '@/components/shadcn/input'
import { Label } from '@/components/shadcn/label'
import { t } from '@/i18n'
import { copyString } from '@/lib/utils'

export interface IShareCreatedProps {
  open: boolean
  link: string
  /** When the link stops working (RFC 3339), from the server; null if unknown. */
  expiresAt: string | null
  /** The link ends at sign-out (iam mode). */
  sessionBound: boolean
  closeHandler: () => void
}

/** The expiry in the viewer's own time zone and locale, or null if unparseable. */
const localTime = (iso: string | null) => {
  if (!iso) return null
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return null
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(at)
}

/**
 * The link that was made, and how long it really lasts: the expiry the server
 * signed it with, never the one that was asked for.
 */
export default function ShareCreated({
  open,
  link,
  expiresAt,
  sessionBound,
  closeHandler
}: IShareCreatedProps) {
  const expiry = localTime(expiresAt)

  return (
    <Dialog open={open} onOpenChange={closeHandler}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t('share_address_title')}</DialogTitle>
          <DialogDescription>
            {expiry
              ? t('share_address_description_created')
              : t('share_address_description')}
          </DialogDescription>
        </DialogHeader>
        <div className="flex items-center space-x-2">
          <div className="grid flex-1 gap-2">
            <Label htmlFor="share-link" className="sr-only">
              Link
            </Label>
            <Input id="share-link" defaultValue={link} readOnly />
          </div>
          <Button
            type="button"
            size="sm"
            className="px-3"
            onClick={() => copyString(link)}
          >
            <span className="sr-only">Copy</span>
            <Copy />
          </Button>
        </div>
        {expiry ? (
          <p data-test="share-expires-at" className="text-sm font-medium">
            {t('share_link_expires_at', { time: expiry })}
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
        <DialogFooter className="sm:justify-start">
          <DialogClose asChild>
            <Button type="button" variant="secondary">
              {t('discard')}
            </Button>
          </DialogClose>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
