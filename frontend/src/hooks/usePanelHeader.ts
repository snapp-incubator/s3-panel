import { useEffect, useState } from 'react'

/** How long the console header may take to draw before the app's own bar shows. */
const HEADER_TIMEOUT_MS = 3000

/**
 * - `pending`: the console header has not reported yet.
 * - `ready`: it has drawn; the app shows no bar of its own.
 * - `fallback`: it failed, or did not report within 3 s; the app shows its own
 *   minimal bar.
 */
export type TPanelHeaderState = 'pending' | 'ready' | 'fallback'

declare global {
  interface Window {
    /** Written by the backend's inline recorder before any app code runs. */
    __panelHeader?: { state: 'pending' | 'ready' | 'failed' }
  }
}

const recorded = (): TPanelHeaderState => {
  const state = window.__panelHeader?.state
  if (state === 'ready') return 'ready'
  if (state === 'failed') return 'fallback'

  return 'pending'
}

/**
 * Tracks the console's shared header (proposal 018 §5), which the backend
 * embeds above the app when the panel is part of a console. The header must
 * never block the app: without its `snappcloud-header:ready` within 3 s of the
 * page loading, the app keeps its own bar. A header that draws later still
 * takes over, so the page never shows both.
 */
const usePanelHeader = (enabled: boolean): TPanelHeaderState => {
  const [state, setState] = useState<TPanelHeaderState>(() =>
    enabled ? recorded() : 'fallback'
  )

  useEffect(() => {
    if (!enabled) return

    const onReady = () => setState('ready')
    const onFailed = () =>
      setState(current => (current === 'ready' ? current : 'fallback'))
    // Capture on window: the events may be dispatched on the element itself,
    // bubbling or not.
    window.addEventListener('snappcloud-header:ready', onReady, true)
    window.addEventListener('snappcloud-header:failed', onFailed, true)

    // Catch up on anything the recorder saw before this ran.
    setState(current => (current === 'ready' ? current : recorded()))

    const timer = window.setTimeout(
      () => setState(current => (current === 'pending' ? 'fallback' : current)),
      Math.max(0, HEADER_TIMEOUT_MS - performance.now())
    )

    return () => {
      window.removeEventListener('snappcloud-header:ready', onReady, true)
      window.removeEventListener('snappcloud-header:failed', onFailed, true)
      window.clearTimeout(timer)
    }
  }, [enabled])

  return state
}

export default usePanelHeader
