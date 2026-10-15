import { useEffect, useState } from "react"

import { m } from "@/i18n"

export const DEFAULT_TIMEZONE = "Europe/Istanbul"

let cachedZones: string[] | undefined

/** Every IANA zone the browser knows, with UTC first. */
export function timezones(): string[] {
  if (!cachedZones) {
    let zones: string[] = []
    try {
      zones = Intl.supportedValuesOf("timeZone")
    } catch {
      zones = [DEFAULT_TIMEZONE]
    }
    cachedZones = ["UTC", ...zones.filter((z) => z !== "UTC")]
  }
  return cachedZones
}

/** The browser's own zone, or the default when it cannot tell. */
export function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || DEFAULT_TIMEZONE
  } catch {
    return DEFAULT_TIMEZONE
  }
}

const formatters = new Map<string, Intl.DateTimeFormat>()

/** Formats an ISO timestamp as a date and time in timeZone. */
export function formatDateTime(iso: string | undefined, timeZone: string): string {
  if (!iso) return m.common.none
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return m.common.none
  let f = formatters.get(timeZone)
  if (!f) {
    try {
      f = new Intl.DateTimeFormat(m.locale, {
        timeZone,
        dateStyle: "medium",
        timeStyle: "short",
      })
    } catch {
      f = new Intl.DateTimeFormat(m.locale, { dateStyle: "medium", timeStyle: "short" })
    }
    formatters.set(timeZone, f)
  }
  return f.format(date)
}

const relative = new Intl.RelativeTimeFormat(m.locale, { numeric: "auto" })

const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
  ["year", 365 * 24 * 3600],
  ["month", 30 * 24 * 3600],
  ["week", 7 * 24 * 3600],
  ["day", 24 * 3600],
  ["hour", 3600],
  ["minute", 60],
  ["second", 1],
]

/** "in 3 hours", "5 minutes ago". */
export function formatRelative(iso: string | undefined, now: number): string {
  if (!iso) return ""
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ""
  const seconds = Math.round((t - now) / 1000)
  const abs = Math.abs(seconds)
  for (const [unit, size] of units) {
    if (abs >= size || unit === "second") {
      return relative.format(Math.round(seconds / size), unit)
    }
  }
  return ""
}

/**
 * Like formatRelative for things that already happened: a time just past
 * a stale `now` (useNow ticks every 30 s) reads "now", not "in 10 seconds".
 */
export function formatAgo(iso: string | undefined, now: number): string {
  if (!iso) return ""
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return ""
  return formatRelative(iso, Math.max(now, t))
}

/** 90 → "1:30", 3600 → "60:00". */
export function formatDuration(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes === 0) return `${seconds}s`
  return `${minutes}:${String(seconds).padStart(2, "0")}`
}

const pad2 = (n: number) => String(n).padStart(2, "0")

/** An elapsed time, at two units' precision: "<1s", "42s", "3m 05s", "2h 05m", "3d 4h". */
export function formatSpan(ms: number): string {
  const s = Math.floor(ms / 1000)
  if (s < 1) return "<1s"
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${pad2(s % 60)}s`
  if (s < 86_400) return `${Math.floor(s / 3600)}h ${pad2(Math.floor(s / 60) % 60)}m`
  return `${Math.floor(s / 86_400)}d ${Math.floor(s / 3600) % 24}h`
}

/** The current time, refreshed every intervalMs, for relative labels. */
export function useNow(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), intervalMs)
    return () => window.clearInterval(id)
  }, [intervalMs])
  return now
}
