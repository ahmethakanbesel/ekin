import { useEffect, useMemo, type ReactNode } from "react"
import { useStore, type DeepKeys } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"

import type { Language, PipelineDefinition } from "@/api/client"
import { TimezoneCombobox } from "@/components/timezone-combobox"
import { Button } from "@/components/ui/button"
import {
  Field,
  FieldContent,
  FieldDescription,
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
import { Textarea } from "@/components/ui/textarea"
import { TemplateSelect } from "@/features/templates/template-select"
import { VoiceOverrideSection } from "@/features/voice/override-section"
import { emptyOverride } from "@/features/voice/model"
import { m } from "@/i18n"
import { useAppForm } from "@/lib/form"
import { documentsQuery } from "@/queries/documents"
import { NumberInput, Row, Section, shownError } from "./form-parts"
import { ProductionSection } from "./production-section"
import { SelfReviewSection } from "./self-review-section"
import {
  LANGUAGES,
  LIMITS,
  chosenKinds,
  hasErrors,
  toDefinition,
  validatePipeline,
  type KnownDocuments,
  type PipelineFormValues,
  type SourceKind,
} from "./model"
import { OutputsFields, outputsFieldMap } from "./outputs-fields"
import { RefsFields } from "./refs-fields"

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

const SOURCE_KINDS: SourceKind[] = ["prompt", "knowledge", "seo"]

/** What the instructions are called per source: a system prompt, guidance or a brief. */
const instructionsCopy: Record<SourceKind, { label: string; hint: string }> = {
  prompt: { label: m.pipelineForm.systemPrompt, hint: m.pipelineForm.systemPromptHint },
  knowledge: { label: m.pipelineForm.guidance, hint: m.pipelineForm.guidanceHint },
  seo: { label: m.pipelineForm.seoBrief, hint: m.pipelineForm.seoBriefHint },
}

const refsFieldMap = { refs: "source.refs" } as const

const languageItems = LANGUAGES.map((l) => ({ value: l, label: m.values.language[l] }))

export function PipelineForm({
  workspaceId,
  initialValues,
  submitLabel,
  banner,
  readOnly = false,
  onSubmit,
}: Props) {
  const documents = useQuery(documentsQuery(workspaceId))
  const known: KnownDocuments | undefined = useMemo(
    () => documents.data && new Map(documents.data.map((d) => [d.id.toLowerCase(), d])),
    [documents.data]
  )
  const form = useAppForm({
    defaultValues: initialValues,
    validators: {
      onChange: ({ value }) => {
        // useForm takes the latest options each render, so this sees the
        // documents loaded since.
        const r = validatePipeline(value, known)
        if (!hasErrors(r)) return undefined
        return {
          form: r.form,
          fields: r.fields as Partial<Record<DeepKeys<PipelineFormValues>, string>>,
        }
      },
    },
    // An invalid form's first submit returns before validating again, and
    // rows added since the last validation (document references) may have
    // lost their errors on remount; validate now so every error shows.
    onSubmitInvalid: ({ formApi }) => void formApi.validate("change"),
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
  const language = useStore(form.store, (s) => s.values.language)
  const sourceKind = useStore(form.store, (s) => s.values.source.kind)
  const scheduled = useStore(form.store, (s) => s.values.scheduled)
  const videoEnabled = useStore(form.store, (s) => s.values.videoEnabled)
  const articleEnabled = useStore(form.store, (s) => s.values.articleEnabled)

  // Re-check the references once the documents arrive (or change status).
  useEffect(() => {
    if (known && sourceKind === "knowledge") void form.validate("change")
  }, [form, known, sourceKind])

  const shown = (meta: { isTouched: boolean; errors: unknown[] }) => shownError(meta, submitted)

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
                // Voices belong to a language.
                form.setFieldValue("voice", emptyOverride())
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
        <OutputsFields
          form={form}
          fields={outputsFieldMap}
          language={language}
          readOnly={readOnly}
          submitted={submitted}
          formError={formError}
          idPrefix="pl"
        />
      </Section>

      {/* House style */}
      <Section
        title={m.pipelineForm.sections.style}
        description={m.pipelineForm.styleHint}
        disabled={readOnly}
      >
        <form.Field name="templateId">
          {(field) => (
            <TemplateSelect
              id="pl-template"
              value={field.state.value}
              onChange={field.handleChange}
              kinds={chosenKinds({ videoEnabled, articleEnabled })}
              disabled={readOnly}
            />
          )}
        </form.Field>
      </Section>

      {/* Voice */}
      <form.Field name="voice">
        {(field) => (
          <VoiceOverrideSection
            idPrefix="pl-voice"
            language={language}
            value={field.state.value}
            onChange={field.handleChange}
            readOnly={readOnly}
          />
        )}
      </form.Field>

      <form.Field name="selfReview">
        {(field) => (
          <SelfReviewSection
            value={field.state.value}
            onChange={field.handleChange}
            readOnly={readOnly}
          />
        )}
      </form.Field>

      <form.Field name="production">
        {(field) => (
          <ProductionSection
            idPrefix="pl-production"
            value={field.state.value}
            onChange={field.handleChange}
            readOnly={readOnly}
          />
        )}
      </form.Field>

      {/* Source */}
      <Section title={m.pipelineForm.sections.source} disabled={readOnly}>
        <FieldGroup className="gap-5">
          <form.Field name="source.kind">
            {(field) => (
              <RadioGroup
                disabled={readOnly}
                value={field.state.value}
                onValueChange={(v) => field.handleChange(v as SourceKind)}
                className="grid gap-3 md:grid-cols-3"
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
                label={instructionsCopy[sourceKind].label}
                htmlFor="pl-instructions"
                hint={instructionsCopy[sourceKind].hint}
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
            <RefsFields
              form={form}
              fields={refsFieldMap}
              workspaceId={workspaceId}
              label={m.pipelineForm.refs}
              hint={m.pipelineForm.refsHint}
              readOnly={readOnly}
              submitted={submitted}
              idPrefix="pl-ref"
            />
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
