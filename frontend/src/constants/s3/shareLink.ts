/**
 * Expiration options for S3 object sharing
 *
 * These values represent the available time periods for which
 * a shared link will remain valid. The 'value' property is sent
 * to the API, while 'title' is displayed to the user. 'seconds' is how
 * long that is, to compare with the longest link the server allows.
 *
 */

export interface IExpirationItem {
  title: string
  value: string
  seconds: number
}

const HOUR = 60 * 60
const DAY = 24 * HOUR

export const expirationItems: IExpirationItem[] = [
  {
    title: '1 Hour',
    value: '1h',
    seconds: HOUR
  },
  {
    title: '6 Hour',
    value: '6h',
    seconds: 6 * HOUR
  },
  {
    title: '12 Hour',
    value: '12h',
    seconds: 12 * HOUR
  },
  {
    title: '1 Day',
    value: '1d',
    seconds: DAY
  },
  {
    title: '3 Day',
    value: '3d',
    seconds: 3 * DAY
  },
  {
    title: '1 Week',
    value: '1w',
    seconds: 7 * DAY
  },
  {
    title: '2 Week',
    value: '2w',
    seconds: 14 * DAY
  }
]

const plural = (n: number, unit: string) => `${n} ${unit}${n === 1 ? '' : 's'}`

/** A duration in whole minutes, e.g. "54 minutes" or "2 hours 5 minutes". */
export function formatShareDuration(seconds: number): string {
  const minutes = Math.floor(seconds / 60)
  const days = Math.floor(minutes / (24 * 60))
  const hours = Math.floor((minutes % (24 * 60)) / 60)
  const rest = minutes % 60
  const parts = [
    days ? plural(days, 'day') : '',
    hours ? plural(hours, 'hour') : '',
    rest && !days ? plural(rest, 'minute') : ''
  ].filter(Boolean)
  return parts.length ? parts.join(' ') : plural(0, 'minute')
}

/**
 * The durations to offer when no link may live longer than maxSeconds (0: no
 * cap, which offers the full list).
 *
 * Every standard duration that fits, plus — when the cap cuts the list short —
 * the cap itself in whole minutes, so a cap under an hour still offers
 * something. Empty when less than a minute is left.
 */
export function shareDurationOptions(maxSeconds: number): IExpirationItem[] {
  if (!maxSeconds || maxSeconds <= 0) return expirationItems
  const fitting = expirationItems.filter(item => item.seconds <= maxSeconds)
  if (fitting.length === expirationItems.length) return fitting

  const minutes = Math.floor(maxSeconds / 60)
  if (minutes < 1) return fitting
  const longest = minutes * 60
  if (fitting.some(item => item.seconds === longest)) return fitting
  return [
    ...fitting,
    {
      title: formatShareDuration(longest),
      value: `${minutes}m`,
      seconds: longest
    }
  ]
}
