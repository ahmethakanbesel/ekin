import { useStore } from "@tanstack/react-form"
import { FileTextIcon, VideoIcon } from "lucide-react"

import type { Language } from "@/api/client"
import { Checkbox } from "@/components/ui/checkbox"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { m } from "@/i18n"
import { withFieldGroup } from "@/lib/form"
import { NumberInput, OutputPanel, Row, shownError } from "./form-parts"
import {
  LANGUAGES,
  LIMITS,
  ORIENTATIONS,
  defaultOutputs,
  toInt,
  type Lead,
  type Orientation,
  type VideoFormat,
} from "./model"

const FORMATS: VideoFormat[] = ["short", "long"]
const LEADS: Lead[] = ["video", "article"]
const formatItems = FORMATS.map((f) => ({ value: f, label: m.values.format[f] }))

/**
 * Where the outputs live in a form: both the pipeline and the request
 * form keep them at the top level under these names.
 */
export const outputsFieldMap = {
  videoEnabled: "videoEnabled",
  video: "video",
  articleEnabled: "articleEnabled",
  article: "article",
  lead: "lead",
} as const

/**
 * The video / article toggles with their specs and, when both are on, the
 * lead. Errors come from the host form's validator (validateOutputs).
 */
export const OutputsFields = withFieldGroup({
  defaultValues: defaultOutputs(),
  props: {
    /** The content language; an article is never translated into it. */
    language: "tr" as Language,
    readOnly: false,
    /** Whether the host form was submitted, so untouched errors show too. */
    submitted: false,
    /** The form-level error (no output chosen). */
    formError: undefined as string | undefined,
    /** Prefix for input ids, unique per form. */
    idPrefix: "out",
  },
  render: function OutputsFieldsGroup({ group, language, readOnly, submitted, formError, idPrefix }) {
    const videoEnabled = useStore(group.store, (s) => s.values.videoEnabled)
    const articleEnabled = useStore(group.store, (s) => s.values.articleEnabled)
    const format = useStore(group.store, (s) => s.values.video.format)
    const shown = (meta: { isTouched: boolean; errors: unknown[] }) => shownError(meta, submitted)
    const id = (name: string) => `${idPrefix}-${name}`

    return (
      <>
        <div className="grid gap-4 lg:grid-cols-2">
          <OutputPanel
            icon={<VideoIcon />}
            title={m.pipelineForm.video}
            hint={m.pipelineForm.videoHint}
            enabled={videoEnabled}
            toggle={
              <group.Field name="videoEnabled">
                {(field) => (
                  <Switch
                    id={id("video")}
                    disabled={readOnly}
                    checked={field.state.value}
                    onCheckedChange={(v) => field.handleChange(v)}
                    aria-label={m.pipelineForm.video}
                  />
                )}
              </group.Field>
            }
          >
            <FieldGroup className="gap-5">
              <group.Field
                name="video.format"
                listeners={{
                  onChange: ({ value }) => {
                    const v = group.getFieldValue("video")
                    if (!ORIENTATIONS[value].includes(v.orientation)) {
                      group.setFieldValue("video.orientation", ORIENTATIONS[value][0])
                    }
                    const [min, max] = value === "short" ? LIMITS.shortSeconds : LIMITS.longSeconds
                    const d = toInt(v.durationSeconds)
                    if (d === undefined || d < min || d > max) {
                      group.setFieldValue("video.durationSeconds", value === "short" ? "60" : "600")
                    }
                  },
                }}
              >
                {(field) => (
                  <Row label={m.pipelineForm.format} htmlFor={id("format")}>
                    <Select
                      items={formatItems}
                      disabled={readOnly}
                      value={field.state.value}
                      onValueChange={(v) => v && field.handleChange(v as VideoFormat)}
                    >
                      <SelectTrigger id={id("format")} className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {formatItems.map((it) => (
                          <SelectItem key={it.value} value={it.value}>
                            {it.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Row>
                )}
              </group.Field>
              <group.Field name="video.orientation">
                {(field) => (
                  <Row label={m.pipelineForm.orientation} error={shown(field.state.meta)}>
                    <RadioGroup
                      disabled={readOnly}
                      value={field.state.value}
                      onValueChange={(v) => field.handleChange(v as Orientation)}
                      className="flex flex-wrap gap-x-5 gap-y-2"
                    >
                      {ORIENTATIONS.long.map((o) => {
                        const allowed = ORIENTATIONS[format].includes(o)
                        return (
                          <Field
                            key={o}
                            orientation="horizontal"
                            className="w-auto"
                            data-disabled={!allowed || undefined}
                          >
                            <RadioGroupItem id={id(`orient-${o}`)} value={o} disabled={!allowed} />
                            <FieldLabel htmlFor={id(`orient-${o}`)} className="font-normal">
                              {m.values.orientation[o]}
                            </FieldLabel>
                          </Field>
                        )
                      })}
                    </RadioGroup>
                  </Row>
                )}
              </group.Field>
              <group.Field name="video.durationSeconds">
                {(field) => (
                  <Row
                    label={m.pipelineForm.durationSeconds}
                    htmlFor={id("duration")}
                    hint={
                      format === "short"
                        ? m.pipelineForm.durationHintShort
                        : m.pipelineForm.durationHintLong
                    }
                    error={shown(field.state.meta)}
                  >
                    <NumberInput
                      id={id("duration")}
                      value={field.state.value}
                      onChange={field.handleChange}
                      onBlur={field.handleBlur}
                      min={format === "short" ? LIMITS.shortSeconds[0] : LIMITS.longSeconds[0]}
                      max={format === "short" ? LIMITS.shortSeconds[1] : LIMITS.longSeconds[1]}
                      invalid={!!shown(field.state.meta)}
                    />
                  </Row>
                )}
              </group.Field>
              {format === "long" ? (
                <group.Field name="video.shortsPerVideo">
                  {(field) => (
                    <Row
                      label={m.pipelineForm.shortsPerVideo}
                      htmlFor={id("shorts")}
                      hint={m.pipelineForm.shortsPerVideoHint}
                      error={shown(field.state.meta)}
                    >
                      <NumberInput
                        id={id("shorts")}
                        value={field.state.value}
                        onChange={field.handleChange}
                        onBlur={field.handleBlur}
                        min={LIMITS.shortsPerVideo[0]}
                        max={LIMITS.shortsPerVideo[1]}
                        invalid={!!shown(field.state.meta)}
                      />
                    </Row>
                  )}
                </group.Field>
              ) : null}
            </FieldGroup>
          </OutputPanel>

          <OutputPanel
            icon={<FileTextIcon />}
            title={m.pipelineForm.article}
            hint={m.pipelineForm.articleHint}
            enabled={articleEnabled}
            toggle={
              <group.Field name="articleEnabled">
                {(field) => (
                  <Switch
                    id={id("article")}
                    disabled={readOnly}
                    checked={field.state.value}
                    onCheckedChange={(v) => field.handleChange(v)}
                    aria-label={m.pipelineForm.article}
                  />
                )}
              </group.Field>
            }
          >
            <FieldGroup className="gap-5">
              <group.Field name="article.words">
                {(field) => (
                  <Row
                    label={m.pipelineForm.words}
                    htmlFor={id("words")}
                    hint={m.pipelineForm.wordsHint}
                    error={shown(field.state.meta)}
                  >
                    <NumberInput
                      id={id("words")}
                      value={field.state.value}
                      onChange={field.handleChange}
                      onBlur={field.handleBlur}
                      min={LIMITS.words[0]}
                      max={LIMITS.words[1]}
                      step={100}
                      invalid={!!shown(field.state.meta)}
                    />
                  </Row>
                )}
              </group.Field>
              <group.Field name="article.translations">
                {(field) => {
                  const targets = LANGUAGES.filter((l) => l !== language)
                  return (
                    <Row label={m.pipelineForm.translations} hint={m.pipelineForm.translationsHint}>
                      {targets.length === 0 ? (
                        <p className="text-sm text-muted-foreground">
                          {m.pipelineForm.noTranslationTargets}
                        </p>
                      ) : (
                        <div className="flex flex-wrap gap-x-5 gap-y-2">
                          {targets.map((l) => (
                            <Field key={l} orientation="horizontal" className="w-auto">
                              <Checkbox
                                id={id(`tr-${l}`)}
                                disabled={readOnly}
                                checked={field.state.value.includes(l)}
                                onCheckedChange={(checked) =>
                                  field.handleChange(
                                    checked
                                      ? [...field.state.value.filter((x) => x !== l), l]
                                      : field.state.value.filter((x) => x !== l)
                                  )
                                }
                              />
                              <FieldLabel htmlFor={id(`tr-${l}`)} className="font-normal">
                                {m.values.language[l]}
                              </FieldLabel>
                            </Field>
                          ))}
                        </div>
                      )}
                    </Row>
                  )
                }}
              </group.Field>
            </FieldGroup>
          </OutputPanel>
        </div>

        {formError ? <FieldError className="mt-3">{formError}</FieldError> : null}

        {videoEnabled && articleEnabled ? (
          <group.Field name="lead">
            {(field) => (
              <Row
                className="mt-5"
                label={m.pipelineForm.lead}
                hint={m.pipelineForm.leadHint}
                error={shown(field.state.meta)}
              >
                <RadioGroup
                  disabled={readOnly}
                  value={field.state.value}
                  onValueChange={(v) => field.handleChange(v as Lead)}
                  className="gap-2"
                >
                  {LEADS.map((l) => (
                    <Field key={l} orientation="horizontal">
                      <RadioGroupItem id={id(`lead-${l}`)} value={l} />
                      <FieldLabel htmlFor={id(`lead-${l}`)} className="font-normal">
                        {l === "video" ? m.pipelineForm.leadVideo : m.pipelineForm.leadArticle}
                      </FieldLabel>
                    </Field>
                  ))}
                </RadioGroup>
              </Row>
            )}
          </group.Field>
        ) : null}
      </>
    )
  },
})
