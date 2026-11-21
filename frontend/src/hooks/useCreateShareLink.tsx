import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { fetchShareLimit, type IShareLinkResponse, shareLink } from '@/api/s3'
import { expirationItems, shareDurationOptions } from '@/constants/s3/shareLink'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'
import { useToast } from './use-toast'
import usePanelSession from './usePanelSession'

interface IUseCreateShareLinkProps {
  open: boolean
  bucket: string
  object: string
  region?: string
  closeHandler: () => void
}

/** How often the limit is re-read while the dialog stays open. */
const LIMIT_REFRESH_MS = 60_000

export default function useCreateShareLink({
  open,
  bucket,
  object,
  region,
  closeHandler
}: IUseCreateShareLinkProps) {
  const [created, setCreated] = useState<IShareLinkResponse | null>(null)
  const { toast } = useToast()
  const [copyAddress, setCopyAddress] = useState(false)
  const [expirationTime, setExpirationTime] = useState<string | null>(null)
  const isIAMMode = usePanelSession(state => state.authMode === 'iam')

  // The longest link the server would sign right now. In iam mode that is what
  // remains of the session credential, which shrinks while the dialog is open,
  // so it is read on every open and refreshed while open.
  const { data: limit, isPending: limitPending } = useQuery({
    queryKey: ['share-limit', region ?? '', bucket],
    queryFn: ({ signal }) => fetchShareLimit(bucket, region, signal),
    enabled: open && !!bucket,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchInterval: open ? LIMIT_REFRESH_MS : false
  })

  // A backend that cannot answer (an older one, or an error) offers the full
  // list, as before: the server still caps the link, and the answer says when
  // it really ends.
  const maxSeconds = limit?.max_seconds ?? 0
  const options = shareDurationOptions(maxSeconds)
  // The cap cuts the usual list short, so the dialog says how far it goes.
  const capped =
    maxSeconds > 0 && expirationItems.some(item => item.seconds > maxSeconds)
  const sessionBound = limit?.session_bound ?? isIAMMode

  // A choice the shrinking limit no longer offers is no choice.
  const selected = options.some(item => item.value === expirationTime)
    ? expirationTime
    : null

  const { mutate, isPending } = useMutation({
    mutationFn: () => shareLink(bucket, object, selected!, region),
    onSuccess: data => {
      setCreated(data)
      closeHandler()
      setCopyAddress(true)
    },
    onError: (err: HTTPClientError<{ message?: string }>) => {
      toast({
        variant: 'destructive',
        title: err.response?.data.message || err.message
      })
      closeHandler()
    }
  })

  return {
    mutate,
    isPending,
    link: created?.url ?? null,
    /** When the created link stops working, from the server's answer. */
    expiresAt: created?.expires_at ?? null,
    /** The created link ends at sign-out (iam mode). */
    createdSessionBound: created?.session_bound ?? isIAMMode,
    copyAddress,
    setCopyAddress,
    expirationTime: selected,
    setExpirationTime,
    options,
    limitPending: open && limitPending,
    maxSeconds,
    capped,
    sessionBound
  }
}
