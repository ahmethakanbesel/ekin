import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import type { ServerProduction } from "@/api/client"
import type {
  DraftEffort,
  FinalFps,
  FinalQuality,
  MediaKind,
  MediaSource,
  PreviewFps,
  PreviewQuality,
  ProductionValues,
  ReviewAt,
} from "@/features/pipelines/model"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { librarySizeQuery } from "@/queries/library"
import { LayeredGroup, LayeredRow, LayeredSelect, type Inherited } from "./layered"

// Production settings as layered rows: shared by Settings › Production
// (over the server), the pipeline form and the request form (over the
// workspace).

/** Claude models offered for drafting; another id set through the API still shows. */
const MODELS = [
  { value: "claude-opus-5-5", label: "Claude Opus 5.5" },
  { value: "claude-sonnet-5-5", label: "Claude Sonnet 5.5" },
  { value: "claude-haiku-5-5", label: "Claude Haiku 5.5" },
  { value: "claude-fable-5-1", label: "Claude Fable 5.1" },
]
const EFFORTS: DraftEffort[] = ["low", "medium", "high", "xhigh", "max"]
const QUALITIES: PreviewQuality[] = ["360p", "540p", "720p", "1080p"]
const FINAL_QUALITIES: FinalQuality[] = ["720p", "1080p", "1440p", "2160p"]
const PREVIEW_RATES: PreviewFps[] = [15, 24, 30]
const FINAL_RATES: FinalFps[] = [30, 60]
const REVIEW_POINTS: ReviewAt[] = ["video", "script"]
const MEDIA_KINDS: MediaKind[] = ["music", "video", "photo"]
const MEDIA_SOURCES: MediaSource[] = ["fresh", "library", "mixed"]
const THUMBNAIL_COUNTS = [0, 1, 2, 3, 4]
const SELF_REVIEW_ROUNDS = [0, 1, 2, 3, 4, 5]

/** The built-ins under the server: what Kurgu does when no layer says. */
const BUILT_IN = { selfReview: 1, reviewAt: "video", thumbnails: 3, media: "fresh" } as const

type Key =
  | "draftModel"
  | "draftEffort"
  | "selfReview"
  | "previewQuality"
  | "finalQuality"
  | "previewFps"
  | "finalFps"
  | "reviewAt"
  | "thumbnails"
  | MediaKey
type MediaKey = `media.${MediaKind}`

/** One resolved value: its label, the layer it comes from, and the raw value. */
export type ResolvedValue = Inherited & { raw: string | number }
/** Every production setting, resolved down to some layer. */
export type Resolved = Record<Key, ResolvedValue>

const t = m.pipelineForm.production

function modelLabel(id: string): string {
  return MODELS.find((it) => it.value === id)?.label ?? id
}

const label = {
  draftModel: (v: string) => modelLabel(v),
  draftEffort: (v: string) => t.efforts[v] ?? v,
  selfReview: (n: number) => (n === 0 ? m.pipelineForm.selfReview.off : m.pipelineForm.selfReview.rounds(n)),
  quality: (v: string) => v,
  fps: (n: number) => m.settings.production.fps(n),
  reviewAt: (v: string) => t.reviewPoints[v] ?? v,
  thumbnails: (n: number) => t.thumbnailCounts(n),
  media: (v: string) => t.mediaSources[v] ?? v,
}

/** What the server and the built-ins give, as Settings › Production inherits it. */
export function serverResolved(server: ServerProduction | undefined): Resolved {
  const S = m.settings.layers.server
  const B = m.settings.layers.builtIn
  const s = server
  const media = (k: MediaKind): [MediaKey, ResolvedValue] => [
    `media.${k}`,
    { value: label.media(BUILT_IN.media), source: B, raw: BUILT_IN.media },
  ]
  return {
    draftModel: s?.draftModel
      ? { value: label.draftModel(s.draftModel), source: S, raw: s.draftModel }
      : { value: m.settings.production.claudeDefault, source: B, raw: "" },
    draftEffort: s?.draftEffort
      ? { value: label.draftEffort(s.draftEffort), source: S, raw: s.draftEffort }
      : { value: m.settings.production.claudeDefault, source: B, raw: "" },
    selfReview: { value: label.selfReview(BUILT_IN.selfReview), source: B, raw: BUILT_IN.selfReview },
    previewQuality: {
      value: s ? label.quality(`${s.previewHeight}p`) : "…",
      source: S,
      raw: s ? `${s.previewHeight}p` : "",
    },
    finalQuality: {
      value: s ? label.quality(`${s.finalHeight}p`) : "…",
      source: S,
      raw: s ? `${s.finalHeight}p` : "",
    },
    previewFps: { value: s ? label.fps(s.previewFps) : "…", source: S, raw: s?.previewFps ?? 30 },
    finalFps: { value: s ? label.fps(s.finalFps) : "…", source: S, raw: s?.finalFps ?? 30 },
    reviewAt: { value: label.reviewAt(BUILT_IN.reviewAt), source: B, raw: BUILT_IN.reviewAt },
    thumbnails: { value: label.thumbnails(BUILT_IN.thumbnails), source: B, raw: BUILT_IN.thumbnails },
    ...Object.fromEntries(MEDIA_KINDS.map(media)),
  } as Resolved
}

/** A layer's values over what it inherits: set values take `source`. */
export function layerResolved(v: ProductionValues, below: Resolved, source: string): Resolved {
  const out = { ...below }
  const put = (k: Key, raw: string | number, text: string) => {
    out[k] = { value: text, source, raw }
  }
  if (v.draftModel) put("draftModel", v.draftModel, label.draftModel(v.draftModel))
  if (v.draftEffort) put("draftEffort", v.draftEffort, label.draftEffort(v.draftEffort))
  if (v.selfReview !== null) put("selfReview", v.selfReview, label.selfReview(v.selfReview))
  if (v.previewQuality) put("previewQuality", v.previewQuality, label.quality(v.previewQuality))
  if (v.finalQuality) put("finalQuality", v.finalQuality, label.quality(v.finalQuality))
  if (v.previewFps) put("previewFps", v.previewFps, label.fps(v.previewFps))
  if (v.finalFps) put("finalFps", v.finalFps, label.fps(v.finalFps))
  if (v.reviewAt) put("reviewAt", v.reviewAt, label.reviewAt(v.reviewAt))
  if (v.thumbnails !== null) put("thumbnails", v.thumbnails, label.thumbnails(v.thumbnails))
  for (const k of MEDIA_KINDS)
    if (v.media[k]) put(`media.${k}`, v.media[k], label.media(v.media[k]))
  return out
}

/** How many settings a layer sets itself, out of how many it shows. */
export function countSet(v: ProductionValues, opts: { video: boolean; longVideo: boolean }) {
  const always = [v.draftModel, v.draftEffort, v.selfReview]
  const video = [v.previewQuality, v.finalQuality, v.previewFps, v.finalFps, v.reviewAt, ...MEDIA_KINDS.map((k) => v.media[k])]
  const shown = [...always, ...(opts.video ? video : []), ...(opts.longVideo ? [v.thumbnails] : [])]
  return {
    set: shown.filter((x) => x !== "" && x !== 0 && x !== null).length,
    total: shown.length,
  }
}

/** The height of a quality label ("720p" → 720); 0 when unknown. */
function height(q: string | number): number {
  return typeof q === "string" ? parseInt(q) || 0 : 0
}

/**
 * The production rows: Drafting, Render, Review and Media. Video-only
 * groups show while a video output is on (always in workspace settings).
 */
export function ProductionFields({
  idPrefix,
  value,
  onChange,
  inherited,
  here,
  video,
  longVideo,
  readOnly = false,
}: {
  idPrefix: string
  value: ProductionValues
  onChange: (next: ProductionValues) => void
  /** What each setting resolves to without this layer. */
  inherited: Resolved
  /** This layer's name, for the "Set here" source ("acme", "This pipeline"). */
  here: string
  video: boolean
  longVideo: boolean
  readOnly?: boolean
}) {
  const set = (patch: Partial<ProductionValues>) => onChange({ ...value, ...patch })
  const effective = layerResolved(value, inherited, here)
  const inheritLabel = (k: Key) =>
    m.settings.inheritOption(String(inherited[k].value))
  const id = (k: string) => `${idPrefix}-${k}`

  // Approving reuses the preview as the final video when the preview is
  // at least as sharp, at the same frame rate.
  const reuses =
    height(effective.previewQuality.raw) >= height(effective.finalQuality.raw) &&
    height(effective.finalQuality.raw) > 0 &&
    effective.previewFps.raw === effective.finalFps.raw

  const row = (
    k: Key,
    text: string,
    current: string,
    options: { value: string; label: string }[],
    write: (raw: string) => void,
    hint?: ReactNode
  ) => (
    <LayeredRow
      key={k}
      label={text}
      htmlFor={id(k)}
      hint={hint}
      inherited={inherited[k]}
      set={current !== ""}
      onReset={() => write("")}
      readOnly={readOnly}
    >
      {(describedBy) => (
        <LayeredSelect
          id={id(k)}
          value={current}
          options={options}
          inheritLabel={inheritLabel(k)}
          describedBy={describedBy}
          disabled={readOnly}
          onChange={write}
        />
      )}
    </LayeredRow>
  )

  return (
    <>
      <LayeredGroup title={m.settings.production.groups.drafting}>
        {row("draftModel", t.model, value.draftModel, MODELS, (v) => set({ draftModel: v }), t.modelHint)}
        {row(
          "draftEffort",
          t.effort,
          value.draftEffort,
          EFFORTS.map((e) => ({ value: e, label: t.efforts[e] })),
          (v) => set({ draftEffort: v as DraftEffort | "" }),
          t.effortHint
        )}
        {row(
          "selfReview",
          m.pipelineForm.selfReview.title,
          value.selfReview === null ? "" : String(value.selfReview),
          SELF_REVIEW_ROUNDS.map((n) => ({ value: String(n), label: label.selfReview(n) })),
          (v) => set({ selfReview: v === "" ? null : Number(v) }),
          m.settings.production.selfReviewHint
        )}
      </LayeredGroup>
      {video ? (
        <LayeredGroup title={m.settings.production.groups.render}>
          {row(
            "previewQuality",
            t.previewQuality,
            value.previewQuality,
            QUALITIES.map((q) => ({ value: q, label: t.qualities[q] })),
            (v) => set({ previewQuality: v as PreviewQuality | "" }),
            reuses ? t.previewFullHint : undefined
          )}
          {row(
            "finalQuality",
            t.finalQuality,
            value.finalQuality,
            FINAL_QUALITIES.map((q) => ({ value: q, label: t.finalQualities[q] })),
            (v) => set({ finalQuality: v as FinalQuality | "" })
          )}
          {row(
            "previewFps",
            t.previewFps,
            value.previewFps ? String(value.previewFps) : "",
            PREVIEW_RATES.map((n) => ({ value: String(n), label: t.previewRates[n] })),
            (v) => set({ previewFps: v ? (Number(v) as PreviewFps) : 0 })
          )}
          {row(
            "finalFps",
            t.finalFps,
            value.finalFps ? String(value.finalFps) : "",
            FINAL_RATES.map((n) => ({ value: String(n), label: t.finalRates[n] })),
            (v) => set({ finalFps: v ? (Number(v) as FinalFps) : 0 })
          )}
        </LayeredGroup>
      ) : null}
      {video ? (
        <LayeredGroup title={m.settings.production.groups.review}>
          {row(
            "reviewAt",
            t.reviewAt,
            value.reviewAt,
            REVIEW_POINTS.map((p) => ({ value: p, label: t.reviewPoints[p] })),
            (v) => set({ reviewAt: v as ReviewAt | "" }),
            t.reviewHints[String(effective.reviewAt.raw)]
          )}
          {longVideo
            ? row(
                "thumbnails",
                t.thumbnails,
                value.thumbnails === null ? "" : String(value.thumbnails),
                THUMBNAIL_COUNTS.map((n) => ({ value: String(n), label: t.thumbnailCounts(n) })),
                (v) => set({ thumbnails: v === "" ? null : Number(v) }),
                t.thumbnailsHint
              )
            : null}
        </LayeredGroup>
      ) : null}
      {video ? (
        <LayeredGroup title={t.media}>
          {MEDIA_KINDS.map((k) =>
            row(
              `media.${k}`,
              t.mediaKinds[k],
              value.media[k],
              MEDIA_SOURCES.map((s) => ({ value: s, label: t.mediaSources[s] })),
              (v) => set({ media: { ...value.media, [k]: v as MediaSource | "" } }),
              <MediaHint kind={k} source={String(effective[`media.${k}`].raw) as MediaSource} />
            )
          )}
        </LayeredGroup>
      ) : null}
    </>
  )
}

/** What a kind's resolved source means: composing, or how much the library holds. */
function MediaHint({ kind, source }: { kind: MediaKind; source: MediaSource }) {
  const workspace = useWorkspace()
  const size = useQuery({ ...librarySizeQuery(workspace.id, kind), enabled: source !== "fresh" })
  if (source === "fresh" || !size.data) return <>{t.freshHints[kind]}</>
  const empty = size.data.count === 0
  return (
    <>
      {empty && source === "library" ? t.emptyLibraryOnly : t.inLibrary(size.data.count, size.data.more)}{" "}
      {empty ? (
        <Link
          to="/w/$workspaceId/library"
          params={{ workspaceId: workspace.id }}
          search={{ kind }}
          className="underline underline-offset-4"
        >
          {t.openLibrary}
        </Link>
      ) : null}
    </>
  )
}
