import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { librarySizeQuery } from "@/queries/library"
import { Row, Section } from "./form-parts"
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
} from "./model"

/** The select value standing for "the workspace default" (a select item needs a value). */
const DEFAULT = "default"

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

/**
 * Whether approving reuses the preview as the final video: it is rendered
 * at the final quality or better, at the final frame rate. Unset values
 * count as the host profile's usual ones, 1080p and 30 fps.
 */
function reusesPreview(v: ProductionValues): boolean {
  return (
    v.previewQuality !== "" &&
    parseInt(v.previewQuality) >= parseInt(v.finalQuality || "1080p") &&
    (v.previewFps || 30) === (v.finalFps || 30)
  )
}

const REVIEW_POINTS: ReviewAt[] = ["video", "script"]

const MEDIA_KINDS: MediaKind[] = ["music", "video", "photo"]

const MEDIA_SOURCES: MediaSource[] = ["fresh", "library", "mixed"]

const THUMBNAIL_COUNTS = [0, 1, 2, 3, 4]

/**
 * "Production" in the pipeline and request forms: how the stories are
 * made, each setting overriding the workspace default. The video-only
 * settings show while a video output is on.
 */
export function ProductionSection({
  idPrefix,
  value,
  onChange,
  video,
  longVideo = false,
  readOnly = false,
}: {
  idPrefix: string
  value: ProductionValues
  onChange: (next: ProductionValues) => void
  video: boolean
  /** A long video output is on: it gets designed thumbnails. */
  longVideo?: boolean
  readOnly?: boolean
}) {
  const t = m.pipelineForm.production
  const models = [{ value: DEFAULT, label: t.workspaceDefault }, ...MODELS]
  if (value.draftModel && !MODELS.some((it) => it.value === value.draftModel)) {
    models.push({ value: value.draftModel, label: value.draftModel })
  }
  const efforts = [
    { value: DEFAULT, label: t.workspaceDefault },
    ...EFFORTS.map((e) => ({ value: e, label: t.efforts[e] })),
  ]
  const qualities = [
    { value: DEFAULT, label: t.workspaceDefault },
    ...QUALITIES.map((q) => ({ value: q, label: t.qualities[q] })),
  ]
  const finalQualities = [
    { value: DEFAULT, label: t.workspaceDefault },
    ...FINAL_QUALITIES.map((q) => ({ value: q, label: t.finalQualities[q] })),
  ]
  const previewRates = [
    { value: DEFAULT, label: t.workspaceDefault },
    ...PREVIEW_RATES.map((n) => ({ value: String(n), label: t.previewRates[n] })),
  ]
  const finalRates = [
    { value: DEFAULT, label: t.workspaceDefault },
    ...FINAL_RATES.map((n) => ({ value: String(n), label: t.finalRates[n] })),
  ]
  const thumbnailCounts = [
    { value: DEFAULT, label: t.thumbnailsDefault },
    ...THUMBNAIL_COUNTS.map((n) => ({ value: String(n), label: t.thumbnailCounts(n) })),
  ]
  const set = (patch: Partial<ProductionValues>) => onChange({ ...value, ...patch })

  return (
    <Section title={t.title} description={t.description} disabled={readOnly}>
      <FieldGroup className="gap-5 sm:grid sm:grid-cols-2">
        <Row label={t.model} htmlFor={`${idPrefix}-model`} hint={t.modelHint}>
          <Select
            items={models}
            disabled={readOnly}
            value={value.draftModel || DEFAULT}
            onValueChange={(v) => v && set({ draftModel: v === DEFAULT ? "" : v })}
          >
            <SelectTrigger id={`${idPrefix}-model`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {models.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
        <Row label={t.effort} htmlFor={`${idPrefix}-effort`} hint={t.effortHint}>
          <Select
            items={efforts}
            disabled={readOnly}
            value={value.draftEffort || DEFAULT}
            onValueChange={(v) =>
              v && set({ draftEffort: v === DEFAULT ? "" : (v as DraftEffort) })
            }
          >
            <SelectTrigger id={`${idPrefix}-effort`} className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {efforts.map((it) => (
                <SelectItem key={it.value} value={it.value}>
                  {it.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Row>
        {video ? (
          <Row
            label={t.previewQuality}
            htmlFor={`${idPrefix}-preview`}
            hint={
              reusesPreview(value)
                ? t.previewFullHint
                : t.previewHint
            }
          >
            <Select
              items={qualities}
              disabled={readOnly}
              value={value.previewQuality || DEFAULT}
              onValueChange={(v) =>
                v && set({ previewQuality: v === DEFAULT ? "" : (v as PreviewQuality) })
              }
            >
              <SelectTrigger id={`${idPrefix}-preview`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {qualities.map((it) => (
                  <SelectItem key={it.value} value={it.value}>
                    {it.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
        ) : null}
        {video ? (
          <Row label={t.finalQuality} htmlFor={`${idPrefix}-final`} hint={t.finalHint}>
            <Select
              items={finalQualities}
              disabled={readOnly}
              value={value.finalQuality || DEFAULT}
              onValueChange={(v) =>
                v && set({ finalQuality: v === DEFAULT ? "" : (v as FinalQuality) })
              }
            >
              <SelectTrigger id={`${idPrefix}-final`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {finalQualities.map((it) => (
                  <SelectItem key={it.value} value={it.value}>
                    {it.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
        ) : null}
        {video ? (
          <Row label={t.previewFps} htmlFor={`${idPrefix}-preview-fps`} hint={t.previewFpsHint}>
            <Select
              items={previewRates}
              disabled={readOnly}
              value={value.previewFps ? String(value.previewFps) : DEFAULT}
              onValueChange={(v) =>
                v && set({ previewFps: v === DEFAULT ? 0 : (Number(v) as PreviewFps) })
              }
            >
              <SelectTrigger id={`${idPrefix}-preview-fps`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {previewRates.map((it) => (
                  <SelectItem key={it.value} value={it.value}>
                    {it.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
        ) : null}
        {video ? (
          <Row label={t.finalFps} htmlFor={`${idPrefix}-final-fps`} hint={t.finalFpsHint}>
            <Select
              items={finalRates}
              disabled={readOnly}
              value={value.finalFps ? String(value.finalFps) : DEFAULT}
              onValueChange={(v) =>
                v && set({ finalFps: v === DEFAULT ? 0 : (Number(v) as FinalFps) })
              }
            >
              <SelectTrigger id={`${idPrefix}-final-fps`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {finalRates.map((it) => (
                  <SelectItem key={it.value} value={it.value}>
                    {it.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
        ) : null}
        {video && longVideo ? (
          <Row label={t.thumbnails} htmlFor={`${idPrefix}-thumbnails`} hint={t.thumbnailsHint}>
            <Select
              items={thumbnailCounts}
              disabled={readOnly}
              value={value.thumbnails === null ? DEFAULT : String(value.thumbnails)}
              onValueChange={(v) => v && set({ thumbnails: v === DEFAULT ? null : Number(v) })}
            >
              <SelectTrigger id={`${idPrefix}-thumbnails`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {thumbnailCounts.map((it) => (
                  <SelectItem key={it.value} value={it.value}>
                    {it.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Row>
        ) : null}
      </FieldGroup>
      {video ? (
        <div className="mt-5 grid gap-2">
          <p className="text-sm font-medium" id={`${idPrefix}-review-label`}>
            {t.reviewAt}
          </p>
          <div
            className="flex flex-wrap gap-1.5"
            role="radiogroup"
            aria-labelledby={`${idPrefix}-review-label`}
          >
            {REVIEW_POINTS.map((point) => (
              <Button
                key={point}
                type="button"
                size="sm"
                variant="outline"
                role="radio"
                aria-checked={value.reviewAt === point}
                className="aria-checked:bg-secondary aria-checked:text-foreground aria-checked:inset-ring aria-checked:inset-ring-foreground/70"
                onClick={() => set({ reviewAt: point })}
              >
                {t.reviewPoints[point]}
              </Button>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">{t.reviewHints[value.reviewAt]}</p>
        </div>
      ) : null}
      {video ? (
        <div className="mt-6 grid gap-3">
          <div className="space-y-1">
            <p className="text-sm font-medium">{t.media}</p>
            <p className="text-xs text-muted-foreground">{t.mediaHint}</p>
          </div>
          <FieldGroup className="gap-5 sm:grid sm:grid-cols-3">
            {MEDIA_KINDS.map((kind) => (
              <MediaRow
                key={kind}
                id={`${idPrefix}-media-${kind}`}
                kind={kind}
                source={value.media[kind]}
                readOnly={readOnly}
                onChange={(source) => set({ media: { ...value.media, [kind]: source } })}
              />
            ))}
          </FieldGroup>
        </div>
      ) : null}
    </Section>
  )
}

/** One kind's media source, with how much of it the library holds. */
function MediaRow({
  id,
  kind,
  source,
  readOnly,
  onChange,
}: {
  id: string
  kind: MediaKind
  source: MediaSource
  readOnly: boolean
  onChange: (source: MediaSource) => void
}) {
  const t = m.pipelineForm.production
  const workspace = useWorkspace()
  const size = useQuery({ ...librarySizeQuery(workspace.id, kind), enabled: source !== "fresh" })
  const items = MEDIA_SOURCES.map((s) => ({ value: s, label: t.mediaSources[s] }))
  let hint: ReactNode = t.freshHints[kind]
  if (source !== "fresh" && size.data) {
    const empty = size.data.count === 0
    hint = (
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
  return (
    <div className="grid gap-2">
      <label htmlFor={id} className="text-sm font-medium">
        {t.mediaKinds[kind]}
      </label>
      <Select items={items} disabled={readOnly} value={source} onValueChange={(v) => v && onChange(v as MediaSource)}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {items.map((it) => (
            <SelectItem key={it.value} value={it.value}>
              {it.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  )
}
