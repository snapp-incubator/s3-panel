import { useDeferredValue, useEffect, useRef, useState } from 'react'

/**
 * A text box whose value lives in the URL.
 *
 * Typing is held in local state and written to the URL as it settles, rather
 * than bound to the URL directly: the router applies a navigation
 * asynchronously, and a controlled input that waits for it drops keystrokes.
 * A URL change that did not come from typing — Back, a link — is copied into
 * the box.
 */
export default function useUrlBackedInput(
  urlValue: string,
  commit: (value: string) => void
) {
  const [text, setText] = useState(urlValue)
  const settled = useDeferredValue(text)

  // Values written to the URL that it has not shown yet, oldest first. A URL
  // value found here is one of ours arriving, not a change to copy back —
  // copying it would undo whatever was typed since.
  const inFlight = useRef<string[]>([])

  useEffect(() => {
    if (settled === urlValue || inFlight.current.includes(settled)) return

    inFlight.current.push(settled)
    commit(settled)
  }, [settled, urlValue, commit])

  useEffect(() => {
    const ours = inFlight.current.indexOf(urlValue)

    if (ours >= 0) {
      inFlight.current.splice(0, ours + 1)

      return
    }

    setText(urlValue)
  }, [urlValue])

  return [text, setText] as const
}
