import type { PipelineDefinition } from "@/api/client"
import { m } from "@/i18n"
import { formatDuration } from "@/lib/time"

export type OutputLine = { kind: "video" | "article"; text: string; lead: boolean }

/** One line per produced deliverable, lead marked when there are two. */
export function outputLines(def: PipelineDefinition): OutputLine[] {
  const { video, article, lead } = def.outputs
  const both = !!video && !!article
  const lines: OutputLine[] = []
  if (video) {
    lines.push({
      kind: "video",
      text:
        m.pipelines.videoSummary(
          m.values.format[video.format] ?? video.format,
          m.values.orientation[video.orientation] ?? video.orientation,
          formatDuration(video.durationSeconds),
        ) + (video.shortsPerVideo ? m.pipelines.shortsSummary(video.shortsPerVideo) : ""),
      lead: both && lead === "video",
    })
  }
  if (article) {
    lines.push({
      kind: "article",
      text: m.pipelines.articleSummary(
        article.words,
        article.translations.map((l) => l.toUpperCase()).join(", "),
      ),
      lead: both && lead === "article",
    })
  }
  return lines
}

const DESCRIPTORS: Record<string, string> = {
  "@daily": "0 0 * * *",
  "@midnight": "0 0 * * *",
  "@weekly": "0 0 * * 0",
  "@hourly": "0 * * * *",
}

/** A cron list of plain numbers and ranges ("1-5", "2,5"), expanded; undefined otherwise. */
function numbers(field: string, min: number, max: number): number[] | undefined {
  const out = new Set<number>()
  for (const part of field.split(",")) {
    const range = /^(\d+)(?:-(\d+))?$/.exec(part)
    if (!range) return undefined
    const from = Number(range[1])
    const to = range[2] === undefined ? from : Number(range[2])
    if (from < min || to > max || from > to) return undefined
    for (let n = from; n <= to; n++) out.add(n)
  }
  return [...out].toSorted((a, b) => a - b)
}

const clock = (hour: number, minute: number) =>
  `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`

/**
 * The common cron shapes in words ("Every day 09:00", "Tue & Fri 10:00").
 * Anything more involved (steps, days of the month, months) returns
 * undefined, and the caller shows the expression alone.
 */
export function scheduleText(cron: string): string | undefined {
  const expr = DESCRIPTORS[cron.trim()] ?? cron.trim()
  const fields = expr.split(/\s+/)
  if (fields.length !== 5) return undefined
  const [min, hour, dom, month, dow] = fields
  if (dom !== "*" || month !== "*") return undefined
  const minutes = numbers(min, 0, 59)
  if (minutes?.length !== 1) return undefined
  const minute = minutes[0]
  const t = m.pipelines.every
  if (hour === "*") return dow === "*" ? t.hour(minute) : undefined
  const hours = numbers(hour, 0, 23)
  if (!hours || hours.length > 4) return undefined
  const times = hours.map((h) => clock(h, minute)).join(", ")
  if (dow === "*") return t.day(times)
  // Cron allows 7 for Sunday as well as 0.
  const days = numbers(dow, 0, 7)?.map((d) => d % 7)
  if (!days) return undefined
  const set = [...new Set(days)].toSorted((a, b) => a - b)
  const key = set.join(",")
  if (key === "0,1,2,3,4,5,6") return t.day(times)
  if (key === "1,2,3,4,5") return t.weekday(times)
  if (key === "0,6") return t.weekend(times)
  if (set.length === 1) return t.week(set[0], times)
  // Monday-first, as the rest of the app reads a week.
  return t.days([...set.filter((d) => d > 0), ...set.filter((d) => d === 0)], times)
}
