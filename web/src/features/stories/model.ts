import type {
  AdHocInput,
  Deliverable,
  DeliverableStatus,
  FeedbackScope,
  Language,
  Measures,
  Story,
} from "@/api/client"
import {
  defaultOutputs,
  toInt,
  toOutputs,
  toRefs,
  validateOutputs,
  validateRefs,
  type KnownDocuments,
  type OutputsValues,
  type PipelineFieldErrors,
  type PipelineValidation,
  type RefValues,
} from "@/features/pipelines/model"
import { emptyOverride, toOverride, type VoiceOverrideValues } from "@/features/voice/model"
import { DEFAULT_SELF_REVIEW } from "@/features/pipelines/self-review-section"
import { m } from "@/i18n"

// ---- List tabs ----

export const STORY_TABS = ["review", "progress", "done", "failed", "all"] as const
export type StoryTab = (typeof STORY_TABS)[number]

/** The summary statuses each tab lists (none: all). */
export const TAB_STATUSES: Record<StoryTab, DeliverableStatus[]> = {
  review: ["review"],
  progress: ["queued", "drafting", "rendering", "finalizing", "publishing", "waiting"],
  done: ["ready", "published"],
  failed: ["failed"],
  all: [],
}

// ---- Deliverables ----

/** "Video · lead", "Article · derived", "Article · EN translation". */
export function deliverableLabel(d: Deliverable): string {
  const kind = m.values.kind[d.kind] ?? d.kind
  if (d.role === "translation") return m.stories.translationLabel(kind, d.language.toUpperCase())
  return m.stories.roleLabel(kind, m.stories.roles[d.role] ?? d.role)
}

/** A rendered video's measured length, "0:58"; undefined until measured. */
export function measuredLength(measured: Measures | undefined): string | undefined {
  return measured?.durationSeconds
    ? formatTimestamp(Math.round(measured.durationSeconds))
    : undefined
}

/** The words a revision says or shows, "1,420 words"; undefined until counted. */
export function measuredWords(measured: Measures | undefined): string | undefined {
  return measured?.words ? m.editor.words(measured.words) : undefined
}

/** The deliverable a derived one or a translation is made from. */
export function sourceOf(story: Story, d: Deliverable): Deliverable | undefined {
  return d.sourceId ? story.deliverables.find((x) => x.id === d.sourceId) : undefined
}

/** Feedback scopes offered per kind; voice, visuals and music only apply to videos. */
export const SCOPES: Record<Deliverable["kind"], FeedbackScope[]> = {
  video: ["all", "script", "voice", "visuals", "music", "metadata"],
  article: ["all", "script", "metadata"],
}

export function scopeLabel(scope: FeedbackScope, kind: Deliverable["kind"]): string {
  if (kind === "article" && scope === "script") return m.review.scopeText
  return m.review.scopes[scope] ?? scope
}

/** 75.4 → "1:15". */
export function formatTimestamp(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds))
  const h = Math.floor(s / 3600)
  const mm = Math.floor((s % 3600) / 60)
  const ss = String(s % 60).padStart(2, "0")
  return h > 0 ? `${h}:${String(mm).padStart(2, "0")}:${ss}` : `${mm}:${ss}`
}

/** "1:15" or "01:02:03" → seconds; undefined when it is not a timestamp. */
export function parseTimestamp(raw: unknown): number | undefined {
  if (typeof raw === "number" && Number.isFinite(raw)) return raw
  if (typeof raw !== "string") return undefined
  const parts = raw.trim().split(":")
  if (parts.length < 1 || parts.length > 3 || parts.some((p) => !/^\d+(\.\d+)?$/.test(p))) {
    return undefined
  }
  return parts.reduce((sum, p) => sum * 60 + Number(p), 0)
}

// ---- Ad-hoc request form ----

export const REQUEST_LIMITS = {
  idea: 5000,
  style: 2000,
  count: [1, 10],
} as const

export type RequestFormValues = OutputsValues & {
  idea: string
  count: string
  language: Language
  style: string
  refs: RefValues[]
  /** The house style; "" for none. */
  templateId: string
  /** Narration voice or speed for these stories; "" for the workspace's. */
  voice: VoiceOverrideValues
  /** Self-review rounds per draft; 0 for none. */
  selfReview: number
}

export function defaultRequestValues(): RequestFormValues {
  return {
    idea: "",
    count: "1",
    language: "tr",
    ...defaultOutputs(),
    style: "",
    refs: [],
    templateId: "",
    voice: emptyOverride(),
    selfReview: DEFAULT_SELF_REVIEW,
  }
}

/** Mirrors the server's checks (production/v1 QueueAdHoc and the spec's limits). */
export function validateRequest(v: RequestFormValues, docs?: KnownDocuments): PipelineValidation {
  const t = m.request.validation
  const fields: PipelineFieldErrors = {}
  const idea = v.idea.trim()
  if (!idea) fields.idea = t.ideaRequired
  else if ([...idea].length > REQUEST_LIMITS.idea) fields.idea = t.ideaTooLong
  const count = toInt(v.count)
  if (count === undefined || count < REQUEST_LIMITS.count[0] || count > REQUEST_LIMITS.count[1]) {
    fields.count = t.count
  }
  if ([...v.style.trim()].length > REQUEST_LIMITS.style) fields.style = t.styleTooLong
  const form = validateOutputs(v, fields)
  validateRefs(v.refs, "refs", fields, { required: false, docs })
  return { form, fields }
}

/** Builds the request body. Call only on values that validate. */
export function toAdHocInput(v: RequestFormValues): AdHocInput {
  const style = v.style.trim()
  return {
    idea: v.idea.trim(),
    count: toInt(v.count) ?? 1,
    language: v.language,
    outputs: toOutputs(v, v.language),
    style: style || undefined,
    refs: v.refs.length > 0 ? toRefs(v.refs) : undefined,
    templateId: v.templateId || undefined,
    voice: toOverride(v.voice),
    selfReviewRounds: v.selfReview,
  }
}
