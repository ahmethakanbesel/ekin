import type { ReactNode } from "react"
import { useForm, useStore, type DeepKeys } from "@tanstack/react-form"
import { Link } from "@tanstack/react-router"
import { FileTextIcon, PlusIcon, Trash2Icon, VideoIcon } from "lucide-react"

import type { Language, PipelineDefinition } from "@/api/client"
import { TimezoneCombobox } from "@/components/timezone-combobox"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldTitle,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import {
  LANGUAGES,
  LIMITS,
  ORIENTATIONS,
  emptyRef,
  hasErrors,
  toDefinition,
  toInt,
  validatePipeline,
  type Lead,
  type Orientation,
  type PipelineFormValues,
  type SourceKind,
  type VideoFormat,
} from "./model"

type Props = {
  workspaceId: string
  initialValues: PipelineFormValues
  submitLabel: string
  /** Shown above the actions: the server's rejection, if any. */
  banner?: ReactNode
  /** Shows the values with every control disabled and no submit. */
  readOnly?: boolean
  onSubmit: (definition: PipelineDefinition) => Promise<void>
}

const FORMATS: VideoFormat[] = ["short", "long"]
const LEADS: Lead[] = ["video", "article"]
const SOURCE_KINDS: SourceKind[] = ["prompt", "knowledge"]

const languageItems = LANGUAGES.map((l) => ({ value: l, label: m.values.language[l] }))
const formatItems = FORMATS.map((f) => ({ value: f, label: m.values.format[f] }))

export function PipelineForm({
  workspaceId,
  initialValues,
  submitLabel,
  banner,
  readOnly = false,
  onSubmit,
}: Props) {
  const form = useForm({
    defaultValues: initialValues,
    validators: {
      onChange: ({ value }) => {
        const r = validatePipeline(value)
        if (!hasErrors(r)) return undefined
        return {
          form: r.form,
          fields: r.fields as Partial<Record<DeepKeys<PipelineFormValues>, string>>,
        }
      },
    },
    onSubmit: async ({ value }) => {
      if (readOnly) return
      await onSubmit(toDefinition(value))
    },
  })

  const submitted = useStore(form.store, (s) => s.submissionAttempts > 0)
  const formError = useStore(form.store, (s) => {
    const e = s.errorMap.onChange
    return typeof e === "string" ? e : undefined
  })
  const videoEnabled = useStore(form.store, (s) => s.values.videoEnabled)
  const articleEnabled = useStore(form.store, (s) => s.values.articleEnabled)
  const format = useStore(form.store, (s) => s.values.video.format)
  const language = useStore(form.store, (s) => s.values.language)
  const sourceKind = useStore(form.store, (s) => s.values.source.kind)
  const scheduled = useStore(form.store, (s) => s.values.scheduled)

  /** The first error of a field, once the user touched it or tried to submit. */
  const shown = (meta: { isTouched: boolean; errors: unknown[] }) =>
    (meta.isTouched || submitted) && meta.errors.length > 0 ? String(meta.errors[0]) : undefined

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      {/* Basics */}
      <Section title={m.pipelineForm.sections.basics} disabled={readOnly}>
        <FieldGroup className="gap-5 md:grid md:grid-cols-[1fr_12rem]">
          <form.Field name="name">
            {(field) => (
              <Row label={m.pipelineForm.name} htmlFor="pl-name" error={shown(field.state.meta)}>
                <Input
                  id="pl-name"
                  autoComplete="off"
                  maxLength={LIMITS.name + 20}
                  placeholder={m.pipelineForm.namePlaceholder}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={!!shown(field.state.meta) || undefined}
                />
              </Row>
            )}
          </form.Field>
          <form.Field
            name="language"
            listeners={{
              onChange: ({ value }) => {
                // An article is never translated into its own language.
                form.setFieldValue("article.translations", (prev) =>
                  prev.filter((l) => l !== value)
                )
              },
            }}
          >
            {(field) => (
              <Row label={m.pipelineForm.language} htmlFor="pl-language">
                <Select
                  items={languageItems}
                  disabled={readOnly}
                  value={field.state.value}
                  onValueChange={(v) => v && field.handleChange(v as Language)}
                >
                  <SelectTrigger id="pl-language" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {languageItems.map((it) => (
                      <SelectItem key={it.value} value={it.value}>
                        {it.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Row>
            )}
          </form.Field>
        </FieldGroup>
      </Section>

      {/* Outputs */}
      <Section
        title={m.pipelineForm.sections.outputs}
        description={m.pipelineForm.outputsHint}
        disabled={readOnly}
      >
        <div className="grid gap-4 lg:grid-cols-2">
          <OutputPanel
            icon={<VideoIcon />}
            title={m.pipelineForm.video}
            hint={m.pipelineForm.videoHint}
            enabled={videoEnabled}
            toggle={
              <form.Field name="videoEnabled">
                {(field) => (
                  <Switch
                    id="pl-video"
                    disabled={readOnly}
                    checked={field.state.value}
                    onCheckedChange={(v) => field.handleChange(v)}
                    aria-label={m.pipelineForm.video}
                  />
                )}
              </form.Field>
            }
          >
            <FieldGroup className="gap-5">
              <form.Field
                name="video.format"
                listeners={{
                  onChange: ({ value }) => {
                    const v = form.getFieldValue("video")
                    if (!ORIENTATIONS[value].includes(v.orientation)) {
                      form.setFieldValue("video.orientation", ORIENTATIONS[value][0])
                    }
                    const [min, max] = value === "short" ? LIMITS.shortSeconds : LIMITS.longSeconds
                    const d = toInt(v.durationSeconds)
                    if (d === undefined || d < min || d > max) {
                      form.setFieldValue("video.durationSeconds", value === "short" ? "60" : "600")
                    }
                  },
                }}
              >
                {(field) => (
                  <Row label={m.pipelineForm.format} htmlFor="pl-format">
                    <Select
                      items={formatItems}
                      disabled={readOnly}
                      value={field.state.value}
                      onValueChange={(v) => v && field.handleChange(v as VideoFormat)}
                    >
                      <SelectTrigger id="pl-format" className="w-full">
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
              </form.Field>
              <form.Field name="video.orientation">
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
                            <RadioGroupItem id={`pl-orient-${o}`} value={o} disabled={!allowed} />
                            <FieldLabel htmlFor={`pl-orient-${o}`} className="font-normal">
                              {m.values.orientation[o]}
                            </FieldLabel>
                          </Field>
                        )
                      })}
                    </RadioGroup>
                  </Row>
                )}
              </form.Field>
              <form.Field name="video.durationSeconds">
                {(field) => (
                  <Row
                    label={m.pipelineForm.durationSeconds}
                    htmlFor="pl-duration"
                    hint={
                      format === "short"
                        ? m.pipelineForm.durationHintShort
                        : m.pipelineForm.durationHintLong
                    }
                    error={shown(field.state.meta)}
                  >
                    <NumberInput
                      id="pl-duration"
                      value={field.state.value}
                      onChange={field.handleChange}
                      onBlur={field.handleBlur}
                      min={format === "short" ? LIMITS.shortSeconds[0] : LIMITS.longSeconds[0]}
                      max={format === "short" ? LIMITS.shortSeconds[1] : LIMITS.longSeconds[1]}
                      invalid={!!shown(field.state.meta)}
                    />
                  </Row>
                )}
              </form.Field>
            </FieldGroup>
          </OutputPanel>

          <OutputPanel
            icon={<FileTextIcon />}
            title={m.pipelineForm.article}
            hint={m.pipelineForm.articleHint}
            enabled={articleEnabled}
            toggle={
              <form.Field name="articleEnabled">
                {(field) => (
                  <Switch
                    id="pl-article"
                    disabled={readOnly}
                    checked={field.state.value}
                    onCheckedChange={(v) => field.handleChange(v)}
                    aria-label={m.pipelineForm.article}
                  />
                )}
              </form.Field>
            }
          >
            <FieldGroup className="gap-5">
              <form.Field name="article.words">
                {(field) => (
                  <Row
                    label={m.pipelineForm.words}
                    htmlFor="pl-words"
                    hint={m.pipelineForm.wordsHint}
                    error={shown(field.state.meta)}
                  >
                    <NumberInput
                      id="pl-words"
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
              </form.Field>
              <form.Field name="article.translations">
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
                                id={`pl-tr-${l}`}
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
                              <FieldLabel htmlFor={`pl-tr-${l}`} className="font-normal">
                                {m.values.language[l]}
                              </FieldLabel>
                            </Field>
                          ))}
                        </div>
                      )}
                    </Row>
                  )
                }}
              </form.Field>
            </FieldGroup>
          </OutputPanel>
        </div>

        {formError ? (
          <FieldError className="mt-3">{formError}</FieldError>
        ) : null}

        {videoEnabled && articleEnabled ? (
          <form.Field name="lead">
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
                      <RadioGroupItem id={`pl-lead-${l}`} value={l} />
                      <FieldLabel htmlFor={`pl-lead-${l}`} className="font-normal">
                        {l === "video" ? m.pipelineForm.leadVideo : m.pipelineForm.leadArticle}
                      </FieldLabel>
                    </Field>
                  ))}
                </RadioGroup>
              </Row>
            )}
          </form.Field>
        ) : null}
      </Section>

      {/* Source */}
      <Section title={m.pipelineForm.sections.source} disabled={readOnly}>
        <FieldGroup className="gap-5">
          <form.Field name="source.kind">
            {(field) => (
              <RadioGroup
                disabled={readOnly}
                value={field.state.value}
                onValueChange={(v) => field.handleChange(v as SourceKind)}
                className="grid gap-3 sm:grid-cols-2"
                aria-label={m.pipelineForm.sourceKind}
              >
                {SOURCE_KINDS.map((k) => (
                  <FieldLabel key={k} htmlFor={`pl-src-${k}`}>
                    <Field orientation="horizontal">
                      <FieldContent>
                        <FieldTitle>{m.pipelineForm.sourceKinds[k]}</FieldTitle>
                        <FieldDescription>{m.pipelineForm.sourceKindHints[k]}</FieldDescription>
                      </FieldContent>
                      <RadioGroupItem id={`pl-src-${k}`} value={k} />
                    </Field>
                  </FieldLabel>
                ))}
              </RadioGroup>
            )}
          </form.Field>

          <form.Field name="source.instructions">
            {(field) => (
              <Row
                label={
                  sourceKind === "prompt" ? m.pipelineForm.systemPrompt : m.pipelineForm.guidance
                }
                htmlFor="pl-instructions"
                hint={
                  sourceKind === "prompt"
                    ? m.pipelineForm.systemPromptHint
                    : m.pipelineForm.guidanceHint
                }
                error={shown(field.state.meta)}
              >
                <Textarea
                  id="pl-instructions"
                  rows={6}
                  className="min-h-36 font-mono text-[13px] md:text-[13px]"
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={!!shown(field.state.meta) || undefined}
                />
              </Row>
            )}
          </form.Field>

          {sourceKind === "knowledge" ? (
            <form.Field name="source.refs" mode="array">
              {(refsField) => (
                <Row
                  label={m.pipelineForm.refs}
                  hint={m.pipelineForm.refsHint}
                  error={shown(refsField.state.meta)}
                >
                  <div className="grid gap-3">
                    {refsField.state.value.map((_, i) => (
                      <div
                        key={i}
                        className="grid items-start gap-2 rounded-3xl border p-3 sm:grid-cols-[1fr_7rem_7rem_auto]"
                      >
                        <form.Field name={`source.refs[${i}].documentId`}>
                          {(field) => (
                            <Field data-invalid={!!shown(field.state.meta) || undefined}>
                              <FieldLabel htmlFor={`pl-ref-${i}-doc`} className="sr-only">
                                {m.pipelineForm.documentId}
                              </FieldLabel>
                              <Input
                                id={`pl-ref-${i}-doc`}
                                placeholder={m.pipelineForm.documentId}
                                className="font-mono text-xs md:text-xs"
                                spellCheck={false}
                                autoComplete="off"
                                value={field.state.value}
                                onChange={(e) => field.handleChange(e.target.value)}
                                onBlur={field.handleBlur}
                                aria-invalid={!!shown(field.state.meta) || undefined}
                              />
                              {shown(field.state.meta) ? (
                                <FieldError>{shown(field.state.meta)}</FieldError>
                              ) : null}
                            </Field>
                          )}
                        </form.Field>
                        <form.Field name={`source.refs[${i}].fromPage`}>
                          {(field) => (
                            <>
                              <FieldLabel htmlFor={`pl-ref-${i}-from`} className="sr-only">
                                {m.pipelineForm.fromPage}
                              </FieldLabel>
                              <NumberInput
                                id={`pl-ref-${i}-from`}
                                placeholder={m.pipelineForm.fromPage}
                                value={field.state.value}
                                onChange={field.handleChange}
                                onBlur={field.handleBlur}
                                min={1}
                                invalid={!!shown(field.state.meta)}
                              />
                            </>
                          )}
                        </form.Field>
                        <form.Field name={`source.refs[${i}].toPage`}>
                          {(field) => (
                            <>
                              <FieldLabel htmlFor={`pl-ref-${i}-to`} className="sr-only">
                                {m.pipelineForm.toPage}
                              </FieldLabel>
                              <NumberInput
                                id={`pl-ref-${i}-to`}
                                placeholder={m.pipelineForm.toPage}
                                value={field.state.value}
                                onChange={field.handleChange}
                                onBlur={field.handleBlur}
                                min={1}
                              />
                            </>
                          )}
                        </form.Field>
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          aria-label={m.pipelineForm.removeRef}
                          onClick={() => refsField.removeValue(i)}
                        >
                          <Trash2Icon />
                        </Button>
                        <form.Subscribe
                          selector={(s) => {
                            // The range error sits on fromPage but concerns both pages.
                            const from = s.fieldMeta[`source.refs[${i}].fromPage`]
                            const to = s.fieldMeta[`source.refs[${i}].toPage`]
                            const touched =
                              from?.isTouched || to?.isTouched || s.submissionAttempts > 0
                            return touched ? (from?.errors[0] as string | undefined) : undefined
                          }}
                        >
                          {(err) =>
                            err ? <FieldError className="sm:col-span-4">{err}</FieldError> : null
                          }
                        </form.Subscribe>
                      </div>
                    ))}
                    <div>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={refsField.state.value.length >= LIMITS.refs}
                        onClick={() => refsField.pushValue(emptyRef())}
                      >
                        <PlusIcon data-icon="inline-start" />
                        {m.pipelineForm.addRef}
                      </Button>
                    </div>
                  </div>
                </Row>
              )}
            </form.Field>
          ) : null}
        </FieldGroup>
      </Section>

      {/* Schedule */}
      <Section
        title={m.pipelineForm.sections.schedule}
        description={m.pipelineForm.scheduleHint}
        disabled={readOnly}
      >
        <FieldGroup className="gap-5">
          <form.Field name="scheduled">
            {(field) => (
              <RadioGroup
                disabled={readOnly}
                value={field.state.value ? "scheduled" : "manual"}
                onValueChange={(v) => field.handleChange(v === "scheduled")}
                className="flex flex-wrap gap-x-6 gap-y-2"
                aria-label={m.pipelineForm.sections.schedule}
              >
                {(["manual", "scheduled"] as const).map((v) => (
                  <Field key={v} orientation="horizontal" className="w-auto">
                    <RadioGroupItem id={`pl-sched-${v}`} value={v} />
                    <FieldLabel htmlFor={`pl-sched-${v}`} className="font-normal">
                      {v === "manual" ? m.pipelineForm.manualOnly : m.pipelineForm.scheduled}
                    </FieldLabel>
                  </Field>
                ))}
              </RadioGroup>
            )}
          </form.Field>

          {scheduled ? (
            <div className="grid gap-5 md:grid-cols-[1fr_16rem_8rem]">
              <form.Field name="schedule.cron">
                {(field) => (
                  <Row
                    label={m.pipelineForm.cron}
                    htmlFor="pl-cron"
                    hint={m.pipelineForm.cronHint}
                    error={shown(field.state.meta)}
                  >
                    <Input
                      id="pl-cron"
                      className="font-mono"
                      spellCheck={false}
                      autoComplete="off"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onBlur={field.handleBlur}
                      aria-invalid={!!shown(field.state.meta) || undefined}
                    />
                    <div className="flex flex-wrap items-center gap-1.5" aria-label={m.pipelineForm.presets}>
                      {m.presets.map((p) => (
                        <Button
                          key={p.cron}
                          type="button"
                          size="xs"
                          variant={field.state.value.trim() === p.cron ? "secondary" : "outline"}
                          onClick={() => field.handleChange(p.cron)}
                        >
                          {p.label}
                        </Button>
                      ))}
                    </div>
                  </Row>
                )}
              </form.Field>
              <form.Field name="schedule.timezone">
                {(field) => (
                  <Row label={m.pipelineForm.timezone} htmlFor="pl-tz" error={shown(field.state.meta)}>
                    <TimezoneCombobox
                      id="pl-tz"
                      disabled={readOnly}
                      value={field.state.value}
                      onChange={field.handleChange}
                      onBlur={field.handleBlur}
                      invalid={!!shown(field.state.meta)}
                    />
                  </Row>
                )}
              </form.Field>
              <form.Field name="schedule.batch">
                {(field) => (
                  <Row
                    label={m.pipelineForm.batch}
                    htmlFor="pl-batch"
                    hint={m.pipelineForm.batchHint}
                    error={shown(field.state.meta)}
                  >
                    <NumberInput
                      id="pl-batch"
                      value={field.state.value}
                      onChange={field.handleChange}
                      onBlur={field.handleBlur}
                      min={LIMITS.batch[0]}
                      max={LIMITS.batch[1]}
                      invalid={!!shown(field.state.meta)}
                    />
                  </Row>
                )}
              </form.Field>
            </div>
          ) : null}
        </FieldGroup>
      </Section>

      {banner}

      <div className="flex flex-wrap justify-end gap-2">
        <Button
          variant="outline"
          render={<Link to="/w/$workspaceId/pipelines" params={{ workspaceId }} />}
          nativeButton={false}
        >
          {readOnly ? m.common.back : m.common.cancel}
        </Button>
        {readOnly ? null : (
          <form.Subscribe selector={(s) => s.isSubmitting}>
            {(submitting) => (
              <Button type="submit" disabled={submitting}>
                {submitting ? m.common.saving : submitLabel}
              </Button>
            )}
          </form.Subscribe>
        )}
      </div>
    </form>
  )
}

function Section({
  title,
  description,
  disabled = false,
  children,
}: {
  title: string
  description?: string
  /** Disables the native inputs and buttons inside (Base UI controls take their own prop). */
  disabled?: boolean
  children: ReactNode
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-base font-medium">{title}</CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
      </CardHeader>
      <CardContent>
        <fieldset disabled={disabled} className="contents">
          {children}
        </fieldset>
      </CardContent>
    </Card>
  )
}

function Row({
  label,
  htmlFor,
  hint,
  error,
  className,
  children,
}: {
  label: string
  htmlFor?: string
  hint?: string
  error?: string
  className?: string
  children: ReactNode
}) {
  return (
    <Field data-invalid={!!error || undefined} className={cn("gap-2", className)}>
      <FieldLabel htmlFor={htmlFor}>{label}</FieldLabel>
      {children}
      {error ? <FieldError>{error}</FieldError> : hint ? <FieldDescription>{hint}</FieldDescription> : null}
    </Field>
  )
}

function OutputPanel({
  icon,
  title,
  hint,
  enabled,
  toggle,
  children,
}: {
  icon: ReactNode
  title: string
  hint: string
  enabled: boolean
  toggle: ReactNode
  children: ReactNode
}) {
  return (
    <div
      className={cn(
        "rounded-3xl border p-4 transition-colors",
        enabled ? "border-primary/40 bg-primary/[0.03]" : "bg-muted/30"
      )}
    >
      <div className="flex items-start gap-3">
        <span className="mt-0.5 text-muted-foreground [&_svg]:size-5">{icon}</span>
        <div className="min-w-0 flex-1">
          <p className="font-medium">{title}</p>
          <p className="text-sm text-muted-foreground">{hint}</p>
        </div>
        {toggle}
      </div>
      {enabled ? <div className="mt-5">{children}</div> : null}
    </div>
  )
}

function NumberInput({
  id,
  value,
  onChange,
  onBlur,
  min,
  max,
  step = 1,
  placeholder,
  invalid,
}: {
  id: string
  value: string
  onChange: (value: string) => void
  onBlur?: () => void
  min?: number
  max?: number
  step?: number
  placeholder?: string
  invalid?: boolean
}) {
  return (
    <Input
      id={id}
      type="number"
      inputMode="numeric"
      className="tabular"
      min={min}
      max={max}
      step={step}
      placeholder={placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onBlur={onBlur}
      aria-invalid={invalid || undefined}
    />
  )
}
