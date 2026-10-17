import type { Document, Language, PipelineDefinition, Schemas } from "@/api/client"

type Outputs = Schemas["Outputs"]
type KnowledgeRef = Schemas["KnowledgeRef"]
type Production = Schemas["Production"]
export type DraftEffort = Schemas["DraftEffort"]
export type PreviewQuality = NonNullable<Production["previewQuality"]>
export type FinalQuality = NonNullable<Production["finalQuality"]>
export type PreviewFps = NonNullable<Production["previewFps"]>
export type FinalFps = NonNullable<Production["finalFps"]>
export type ReviewAt = NonNullable<Production["reviewAt"]>
export type MediaSource = Schemas["MediaSource"]
export type MediaKind = "music" | "video" | "photo"
import { emptyOverride, fromOverride, toOverride, type VoiceOverrideValues } from "@/features/voice/model"
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
  shortsPerVideo: [0, 20],
  instructions: 20000,
  refs: 50,
  batch: [1, 20],
} as const

export type RefValues = { documentId: string; fromPage: string; toPage: string }

/**
 * The outputs part of a form (pipelines and ad-hoc requests share it; see
 * outputs-fields.tsx). Both sections are always present so toggling one
 * off and on again keeps what was typed.
 */
export type OutputsValues = {
  videoEnabled: boolean
  /** shortsPerVideo: shorts cut from a long video once live; "0" for none. */
  video: { format: VideoFormat; orientation: Orientation; durationSeconds: string; shortsPerVideo: string }
  articleEnabled: boolean
  article: { words: string; translations: Language[] }
  lead: Lead
}

export type PipelineFormValues = OutputsValues & {
  name: string
  language: Language
  source: { kind: SourceKind; instructions: string; refs: RefValues[] }
  scheduled: boolean
  schedule: { cron: string; timezone: string; batch: string }
  /** The house style; "" for none. */
  templateId: string
  /** Narration voice or speed for this pipeline; "" for the workspace's. */
  voice: VoiceOverrideValues
  /** How the stories are made; unset fields take the workspace default. */
  production: ProductionValues
}

/**
 * How stories are made, at one layer (workspace, pipeline or request; see
 * settings/production-fields.tsx). "", 0 and null are unset: the layer
 * above decides.
 */
export type ProductionValues = {
  draftModel: string
  draftEffort: DraftEffort | ""
  /** Self-review rounds per draft, 0 for none. */
  selfReview: number | null
  previewQuality: PreviewQuality | ""
  finalQuality: FinalQuality | ""
  /** Frames per second. */
  previewFps: PreviewFps | 0
  finalFps: FinalFps | 0
  reviewAt: ReviewAt | ""
  /** Where each kind of media comes from. */
  media: Record<MediaKind, MediaSource | "">
  /** Thumbnail variants a long video gets, 0 for none. */
  thumbnails: number | null
}

export function emptyProduction(): ProductionValues {
  return {
    draftModel: "",
    draftEffort: "",
    selfReview: null,
    previewQuality: "",
    finalQuality: "",
    previewFps: 0,
    finalFps: 0,
    reviewAt: "",
    media: { music: "", video: "", photo: "" },
    thumbnails: null,
  }
}

export function fromProduction(p: Production | undefined, selfReviewRounds?: number): ProductionValues {
  return {
    draftModel: p?.draftModel ?? "",
    draftEffort: p?.draftEffort ?? "",
    selfReview: selfReviewRounds ?? null,
    previewQuality: p?.previewQuality ?? "",
    finalQuality: p?.finalQuality ?? "",
    previewFps: p?.previewFps ?? 0,
    finalFps: p?.finalFps ?? 0,
    reviewAt: p?.reviewAt ?? "",
    media: { music: p?.media?.music ?? "", video: p?.media?.video ?? "", photo: p?.media?.photo ?? "" },
    thumbnails: p?.thumbnails ?? null,
  }
}

/** The wire production settings (self-review travels apart); undefined when none is set. */
export function toProduction(v: ProductionValues): Production | undefined {
  const out: Production = {}
  if (v.draftModel) out.draftModel = v.draftModel
  if (v.draftEffort) out.draftEffort = v.draftEffort
  if (v.previewQuality) out.previewQuality = v.previewQuality
  if (v.finalQuality) out.finalQuality = v.finalQuality
  if (v.previewFps) out.previewFps = v.previewFps
  if (v.finalFps) out.finalFps = v.finalFps
  if (v.reviewAt) out.reviewAt = v.reviewAt
  const media: NonNullable<Production["media"]> = {}
  for (const k of ["music", "video", "photo"] as const) if (v.media[k]) media[k] = v.media[k]
  if (Object.keys(media).length > 0) out.media = media
  if (v.thumbnails !== null) out.thumbnails = v.thumbnails
  return Object.keys(out).length > 0 ? out : undefined
}

/** Self-review rounds on the wire; undefined inherits. */
export function toSelfReview(v: ProductionValues): number | undefined {
  return v.selfReview ?? undefined
}

export function emptyRef(): RefValues {
  return { documentId: "", fromPage: "", toPage: "" }
}

export function defaultOutputs(): OutputsValues {
  return {
    videoEnabled: true,
    video: { format: "short", orientation: "vertical", durationSeconds: "60", shortsPerVideo: "0" },
    articleEnabled: false,
    article: { words: "1200", translations: [] },
    lead: "video",
  }
}

export function defaultValues(workspaceTimezone: string): PipelineFormValues {
  return {
    name: "",
    language: "tr",
    ...defaultOutputs(),
    source: { kind: "prompt", instructions: "", refs: [] },
    scheduled: false,
    schedule: { cron: "0 2 * * *", timezone: workspaceTimezone, batch: "1" },
    templateId: "",
    voice: emptyOverride(),
    production: emptyProduction(),
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
          shortsPerVideo: String(video.shortsPerVideo ?? 0),
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
    templateId: def.templateId ?? "",
    voice: fromOverride(def.voice),
    production: fromProduction(def.production, def.selfReviewRounds),
  }
}

/** The wire outputs of a form. Call only on values that validate. */
export function toOutputs(v: OutputsValues, language: Language): Outputs {
  const both = v.videoEnabled && v.articleEnabled
  return {
    video: v.videoEnabled
      ? {
          format: v.video.format,
          orientation: v.video.orientation,
          durationSeconds: toInt(v.video.durationSeconds) ?? 0,
          shortsPerVideo:
            v.video.format === "long" && (toInt(v.video.shortsPerVideo) ?? 0) > 0
              ? toInt(v.video.shortsPerVideo)
              : undefined,
        }
      : undefined,
    article: v.articleEnabled
      ? {
          words: toInt(v.article.words) ?? 0,
          translations: v.article.translations.filter((l) => l !== language),
        }
      : undefined,
    lead: both ? v.lead : v.videoEnabled ? "video" : "article",
  }
}

/** The wire references of reference rows; blank pages mean the whole document. */
export function toRefs(refs: RefValues[]): KnowledgeRef[] {
  return refs.map((r) => {
    const from = toInt(r.fromPage)
    const to = toInt(r.toPage)
    return {
      documentId: r.documentId.trim(),
      ...(from !== undefined && to !== undefined ? { fromPage: from, toPage: to } : {}),
    }
  })
}

/** Builds the wire definition. Call only on values that validate. */
export function toDefinition(v: PipelineFormValues): PipelineDefinition {
  const def: PipelineDefinition = {
    name: v.name.trim(),
    language: v.language,
    outputs: toOutputs(v, v.language),
    source: {
      kind: v.source.kind,
      instructions: v.source.instructions.trim(),
      refs: v.source.kind === "knowledge" ? toRefs(v.source.refs) : undefined,
    },
    schedule: v.scheduled
      ? {
          cron: v.schedule.cron.trim().split(/\s+/).join(" "),
          timezone: v.schedule.timezone,
          batch: toInt(v.schedule.batch) ?? 0,
        }
      : undefined,
    templateId: v.templateId || undefined,
    voice: toOverride(v.voice),
    selfReviewRounds: toSelfReview(v.production),
    production: toProduction(v.production),
  }
  return def
}

/** The output kinds a form has switched on. */
export function chosenKinds(v: Pick<OutputsValues, "videoEnabled" | "articleEnabled">): Lead[] {
  const kinds: Lead[] = []
  if (v.videoEnabled) kinds.push("video")
  if (v.articleEnabled) kinds.push("article")
  return kinds
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

/** What validation needs to know about the workspace's documents, by id. */
export type KnownDocuments = ReadonlyMap<string, Pick<Document, "status" | "pageCount">>

/**
 * Mirrors the backend's domain rules (internal/domain/pipeline/v1, and
 * knowledge/v1 CheckRange once `docs` — the loaded knowledge base — is
 * given: a referenced document must exist, be ready and have the pages).
 */
export function validatePipeline(v: PipelineFormValues, docs?: KnownDocuments): PipelineValidation {
  const t = m.validation
  const fields: PipelineFieldErrors = {}

  const name = v.name.trim()
  if (!name) fields.name = t.nameRequired
  else if ([...name].length > LIMITS.name) fields.name = t.nameTooLong

  const form = validateOutputs(v, fields)

  const instructions = v.source.instructions.trim()
  if ([...instructions].length > LIMITS.instructions) {
    fields["source.instructions"] = t.instructionsTooLong
  } else if (v.source.kind === "prompt" && !instructions) {
    fields["source.instructions"] = t.instructionsRequired
  } else if (v.source.kind === "seo" && !instructions) {
    fields["source.instructions"] = t.briefRequired
  }

  if (v.source.kind === "knowledge") {
    validateRefs(v.source.refs, "source.refs", fields, { required: true, docs })
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

/**
 * Checks the outputs part of a form, adding field errors (keyed `video.…`,
 * `article.…`) to fields. Returns the error not tied to one input: no
 * output chosen.
 */
export function validateOutputs(v: OutputsValues, fields: PipelineFieldErrors): string | undefined {
  const t = m.validation
  if (v.videoEnabled) {
    if (v.video.format === "short" && !ORIENTATIONS.short.includes(v.video.orientation)) {
      fields["video.orientation"] = t.shortOrientation
    }
    const short = v.video.format === "short"
    if (!inRange(v.video.durationSeconds, short ? LIMITS.shortSeconds : LIMITS.longSeconds)) {
      fields["video.durationSeconds"] = short ? t.durationShort : t.durationLong
    }
    if (!short && !inRange(v.video.shortsPerVideo, LIMITS.shortsPerVideo)) {
      fields["video.shortsPerVideo"] = t.shortsPerVideo
    }
  }
  if (v.articleEnabled && !inRange(v.article.words, LIMITS.words)) {
    fields["article.words"] = t.words
  }
  return !v.videoEnabled && !v.articleEnabled ? t.outputRequired : undefined
}

/**
 * Checks document reference rows at `path` (e.g. "source.refs"), adding
 * errors keyed `${path}[i].documentId` / `${path}[i].fromPage` and, for
 * the list itself, `path`. With `docs` — the loaded knowledge base — it
 * also mirrors knowledge/v1 CheckRange: a referenced document must exist,
 * be ready and have the pages.
 */
export function validateRefs(
  refs: RefValues[],
  path: string,
  fields: PipelineFieldErrors,
  { required, docs }: { required: boolean; docs?: KnownDocuments },
): void {
  const t = m.validation
  if (required && refs.length === 0) fields[path] = t.refsRequired
  else if (refs.length > LIMITS.refs) fields[path] = t.refsTooMany
  const ranges: Array<{ doc: string; from: number; to: number } | undefined> = []
  refs.forEach((r, i) => {
    const doc = r.documentId.trim().toLowerCase()
    const known = docs?.get(doc)
    if (!UUID.test(doc)) fields[`${path}[${i}].documentId`] = t.documentId
    else if (docs && !known) fields[`${path}[${i}].documentId`] = t.documentMissing
    else if (known && known.status !== "ready") {
      fields[`${path}[${i}].documentId`] = t.documentNotReady
    }
    const fromBlank = r.fromPage.trim() === ""
    const toBlank = r.toPage.trim() === ""
    if (fromBlank && toBlank) {
      ranges.push({ doc, from: 0, to: 0 })
      return
    }
    const from = toInt(r.fromPage)
    const to = toInt(r.toPage)
    if (from === undefined || to === undefined || from < 1 || to < from) {
      fields[`${path}[${i}].fromPage`] = t.pageRange
      ranges.push(undefined)
      return
    }
    if (known?.status === "ready" && to > known.pageCount) {
      fields[`${path}[${i}].fromPage`] = t.pageBeyond(known.pageCount)
    }
    ranges.push({ doc, from, to })
  })
  ranges.forEach((a, i) => {
    if (!a || fields[`${path}[${i}].documentId`]) return
    for (let j = 0; j < i; j++) {
      const b = ranges[j]
      if (!b || b.doc !== a.doc) continue
      const whole = a.from === 0 || b.from === 0
      if (whole || (a.from <= b.to && b.from <= a.to)) {
        fields[`${path}[${i}].fromPage`] = t.pageOverlap
        return
      }
    }
  })
}

export function hasErrors(r: PipelineValidation): boolean {
  return !!r.form || Object.keys(r.fields).length > 0
}
