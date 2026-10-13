import type { Language, PipelineDefinition, Schemas } from "@/api/client"
import { m } from "@/i18n"

// The editable shape of a pipeline. Both output sections are always
// present so toggling one off and on again keeps what was typed; only the
// enabled ones are sent. Numeric inputs are kept as strings so a field
// can be momentarily empty while typing.

export const LANGUAGES: Language[] = ["tr", "en"]
export type VideoFormat = Schemas["VideoSpec"]["format"]
export type Orientation = Schemas["VideoSpec"]["orientation"]
export type Lead = Schemas["DeliverableKind"]
export type SourceKind = Schemas["Source"]["kind"]

export const ORIENTATIONS: Record<VideoFormat, Orientation[]> = {
  short: ["vertical", "square"],
  long: ["horizontal", "vertical", "square"],
}

export const LIMITS = {
  name: 120,
  shortSeconds: [10, 180],
  longSeconds: [60, 3600],
  words: [300, 6000],
  instructions: 20000,
  refs: 50,
  batch: [1, 20],
} as const

export type RefValues = { documentId: string; fromPage: string; toPage: string }

export type PipelineFormValues = {
  name: string
  language: Language
  videoEnabled: boolean
  video: { format: VideoFormat; orientation: Orientation; durationSeconds: string }
  articleEnabled: boolean
  article: { words: string; translations: Language[] }
  lead: Lead
  source: { kind: SourceKind; instructions: string; refs: RefValues[] }
  scheduled: boolean
  schedule: { cron: string; timezone: string; batch: string }
}

export function emptyRef(): RefValues {
  return { documentId: "", fromPage: "", toPage: "" }
}

export function defaultValues(workspaceTimezone: string): PipelineFormValues {
  return {
    name: "",
    language: "tr",
    videoEnabled: true,
    video: { format: "short", orientation: "vertical", durationSeconds: "60" },
    articleEnabled: false,
    article: { words: "1200", translations: [] },
    lead: "video",
    source: { kind: "prompt", instructions: "", refs: [] },
    scheduled: false,
    schedule: { cron: "0 2 * * *", timezone: workspaceTimezone, batch: "1" },
  }
}

export function fromDefinition(
  def: PipelineDefinition,
  workspaceTimezone: string,
): PipelineFormValues {
  const base = defaultValues(workspaceTimezone)
  const { video, article, lead } = def.outputs
  return {
    name: def.name,
    language: def.language,
    videoEnabled: !!video,
    video: video
      ? {
          format: video.format,
          orientation: video.orientation,
          durationSeconds: String(video.durationSeconds),
        }
      : base.video,
    articleEnabled: !!article,
    article: article
      ? { words: String(article.words), translations: [...article.translations] }
      : base.article,
    lead: lead ?? (video ? "video" : "article"),
    source: {
      kind: def.source.kind,
      instructions: def.source.instructions,
      refs: (def.source.refs ?? []).map((r) => ({
        documentId: r.documentId,
        fromPage: r.fromPage ? String(r.fromPage) : "",
        toPage: r.toPage ? String(r.toPage) : "",
      })),
    },
    scheduled: !!def.schedule,
    schedule: def.schedule
      ? {
          cron: def.schedule.cron,
          timezone: def.schedule.timezone,
          batch: String(def.schedule.batch),
        }
      : base.schedule,
  }
}

/** Builds the wire definition. Call only on values that validate. */
export function toDefinition(v: PipelineFormValues): PipelineDefinition {
  const both = v.videoEnabled && v.articleEnabled
  const def: PipelineDefinition = {
    name: v.name.trim(),
    language: v.language,
    outputs: {
      video: v.videoEnabled
        ? {
            format: v.video.format,
            orientation: v.video.orientation,
            durationSeconds: toInt(v.video.durationSeconds) ?? 0,
          }
        : undefined,
      article: v.articleEnabled
        ? {
            words: toInt(v.article.words) ?? 0,
            translations: v.article.translations.filter((l) => l !== v.language),
          }
        : undefined,
      lead: both ? v.lead : v.videoEnabled ? "video" : "article",
    },
    source: {
      kind: v.source.kind,
      instructions: v.source.instructions.trim(),
      refs:
        v.source.kind === "knowledge"
          ? v.source.refs.map((r) => {
              const from = toInt(r.fromPage)
              const to = toInt(r.toPage)
              return {
                documentId: r.documentId.trim(),
                ...(from !== undefined && to !== undefined ? { fromPage: from, toPage: to } : {}),
              }
            })
          : undefined,
    },
    schedule: v.scheduled
      ? {
          cron: v.schedule.cron.trim().split(/\s+/).join(" "),
          timezone: v.schedule.timezone,
          batch: toInt(v.schedule.batch) ?? 0,
        }
      : undefined,
  }
  return def
}

/** A whole number from a form string, or undefined when blank or not one. */
export function toInt(s: string): number | undefined {
  const t = s.trim()
  if (!/^\d+$/.test(t)) return undefined
  return Number.parseInt(t, 10)
}

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i

function inRange(s: string, [min, max]: readonly [number, number]): boolean {
  const n = toInt(s)
  return n !== undefined && n >= min && n <= max
}

// Field paths as TanStack Form names them.
export type PipelineFieldErrors = Partial<Record<string, string>>

export type PipelineValidation = {
  /** Errors not tied to one input (the output choice). */
  form?: string
  fields: PipelineFieldErrors
}

/** Mirrors the backend's domain rules (internal/domain/pipeline/v1). */
export function validatePipeline(v: PipelineFormValues): PipelineValidation {
  const t = m.validation
  const fields: PipelineFieldErrors = {}
  let form: string | undefined

  const name = v.name.trim()
  if (!name) fields.name = t.nameRequired
  else if ([...name].length > LIMITS.name) fields.name = t.nameTooLong

  if (!v.videoEnabled && !v.articleEnabled) form = t.outputRequired

  if (v.videoEnabled) {
    if (v.video.format === "short" && !ORIENTATIONS.short.includes(v.video.orientation)) {
      fields["video.orientation"] = t.shortOrientation
    }
    const short = v.video.format === "short"
    if (!inRange(v.video.durationSeconds, short ? LIMITS.shortSeconds : LIMITS.longSeconds)) {
      fields["video.durationSeconds"] = short ? t.durationShort : t.durationLong
    }
  }

  if (v.articleEnabled && !inRange(v.article.words, LIMITS.words)) {
    fields["article.words"] = t.words
  }

  const instructions = v.source.instructions.trim()
  if ([...instructions].length > LIMITS.instructions) {
    fields["source.instructions"] = t.instructionsTooLong
  } else if (v.source.kind === "prompt" && !instructions) {
    fields["source.instructions"] = t.instructionsRequired
  }

  if (v.source.kind === "knowledge") {
    const refs = v.source.refs
    if (refs.length === 0) fields["source.refs"] = t.refsRequired
    else if (refs.length > LIMITS.refs) fields["source.refs"] = t.refsTooMany
    const ranges: Array<{ doc: string; from: number; to: number } | undefined> = []
    refs.forEach((r, i) => {
      const doc = r.documentId.trim().toLowerCase()
      if (!UUID.test(doc)) fields[`source.refs[${i}].documentId`] = t.documentId
      const fromBlank = r.fromPage.trim() === ""
      const toBlank = r.toPage.trim() === ""
      if (fromBlank && toBlank) {
        ranges.push({ doc, from: 0, to: 0 })
        return
      }
      const from = toInt(r.fromPage)
      const to = toInt(r.toPage)
      if (from === undefined || to === undefined || from < 1 || to < from) {
        fields[`source.refs[${i}].fromPage`] = t.pageRange
        ranges.push(undefined)
        return
      }
      ranges.push({ doc, from, to })
    })
    ranges.forEach((a, i) => {
      if (!a || fields[`source.refs[${i}].documentId`]) return
      for (let j = 0; j < i; j++) {
        const b = ranges[j]
        if (!b || b.doc !== a.doc) continue
        const whole = a.from === 0 || b.from === 0
        if (whole || (a.from <= b.to && b.from <= a.to)) {
          fields[`source.refs[${i}].fromPage`] = t.pageOverlap
          return
        }
      }
    })
  }

  if (v.scheduled) {
    const cron = v.schedule.cron.trim()
    if (!cron) fields["schedule.cron"] = t.cronRequired
    else if (cron.includes("TZ=")) fields["schedule.cron"] = t.cronTz
    else if (cron.startsWith("@every")) fields["schedule.cron"] = t.cronEvery
    else if (!cron.startsWith("@") && cron.split(/\s+/).length !== 5) {
      fields["schedule.cron"] = t.cronFields
    }
    if (!v.schedule.timezone) fields["schedule.timezone"] = t.timezoneRequired
    if (!inRange(v.schedule.batch, LIMITS.batch)) fields["schedule.batch"] = t.batch
  }

  return { form, fields }
}

export function hasErrors(r: PipelineValidation): boolean {
  return !!r.form || Object.keys(r.fields).length > 0
}
