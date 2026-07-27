import { type EffectCallback, useEffect, useRef, useState } from 'react'

const useEffectOnce = (effect: EffectCallback) => {
  const effectFn = useRef<EffectCallback>(effect)
  const destroyFn = useRef<ReturnType<EffectCallback>>(undefined)
  const effectCalled = useRef(false)
  const rendered = useRef(false)
  const [, setVal] = useState<number>(0)

  if (effectCalled.current) {
    rendered.current = true
  }

  useEffect(() => {
    if (!effectCalled.current) {
      destroyFn.current = effectFn.current()
      effectCalled.current = true
    }

    setVal(val => val + 1)

    return () => {
      if (!rendered.current) {
        return
      }

      if (destroyFn.current) {
        destroyFn.current()
      }
    }
  }, [])
}

export default useEffectOnce
