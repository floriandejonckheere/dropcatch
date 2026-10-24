import type { Status } from './api'

export const statusLabel: Record<Status, string> = {
  unknown: 'Not checked',
  registered: 'Registered',
  dropping: 'Dropping',
  available: 'Available',
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })

/** "5 minutes ago", "in 3 days". */
export function relative(iso: string | undefined, now = Date.now()): string {
  if (!iso) return ''
  const s = (new Date(iso).getTime() - now) / 1000
  // now ticks every 30 seconds, so recent times can be slightly ahead of it.
  if (Math.abs(s) < 60) return 'just now'
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 365 * 86400],
    ['month', 30 * 86400],
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, size] of units) {
    if (Math.abs(s) >= size) return rtf.format(Math.round(s / size), unit)
  }
  return 'just now'
}

export function date(iso: string | undefined): string {
  return iso ? new Date(iso).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' }) : ''
}

export function dateTime(iso: string | undefined): string {
  return iso ? new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }) : ''
}

/** Days until iso (negative when past). */
export function daysUntil(iso: string, now = Date.now()): number {
  return Math.floor((new Date(iso).getTime() - now) / 86400000)
}

/** "1 hour", "15 minutes", "2 days". */
export function minutes(n: number): string {
  if (n % 1440 === 0) return n === 1440 ? '1 day' : `${n / 1440} days`
  if (n % 60 === 0) return n === 60 ? '1 hour' : `${n / 60} hours`
  return n === 1 ? '1 minute' : `${n} minutes`
}

/** Hides the credentials in an Apprise URL: "tgram://123…/…". */
export function maskUrl(u: string): string {
  const i = u.indexOf('://')
  if (i < 0) return u
  const rest = u.slice(i + 3)
  return `${u.slice(0, i + 3)}${rest.slice(0, 4)}${rest.length > 4 ? '…' : ''}`
}
