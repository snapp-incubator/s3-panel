import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'

import { shareLink } from '@/api/s3'
import type { HTTPClientError } from '@/services/http/interceptorsConfig'

import { useToast } from './use-toast'

interface IUseCreateShareLinkProps {
  bucket: string
  object: string
  region?: string
  closeHandler: () => void
}

export default function useCreateShareLink({
  bucket,
  object,
  region,
  closeHandler
}: IUseCreateShareLinkProps) {
  const [link, setLink] = useState<string | null>(null)
  const { toast } = useToast()
  const [copyAddress, setCopyAddress] = useState(false)
  const [expirationTime, setExpirationTime] = useState<string | null>(null)

  const { mutate } = useMutation({
    mutationFn: () => shareLink(bucket, object, expirationTime!, region),
    onSuccess: data => {
      setLink(data.url)
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
    link,
    copyAddress,
    setCopyAddress,
    expirationTime,
    setExpirationTime
  }
}
