import { useEffect, useMemo, useState } from "react"
import { useStore, type DeepKeys } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import type { Language } from "@/api/client"
import { Banner, EmptyState, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { FieldGroup } from "@/components/ui/field"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { NumberInput, Row, Section, shownError } from "@/features/pipelines/form-parts"
import { LANGUAGES, chosenKinds, hasErrors, type KnownDocuments } from "@/features/pipelines/model"
import { OutputsFields, outputsFieldMap } from "@/features/pipelines/outputs-fields"
import { RefsFields } from "@/features/pipelines/refs-fields"
import { TemplateSelect } from "@/features/templates/template-select"
import { emptyOverride } from "@/features/voice/model"
import { ProductionSection } from "@/features/pipelines/production-section"
import { SelfReviewSection } from "@/features/pipelines/self-review-section"
import { VoiceOverrideSection } from "@/features/voice/override-section"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useAppForm } from "@/lib/form"
import { formatInteger } from "@/lib/format"
import { cn } from "@/lib/utils"
import { allDocumentsQuery } from "@/queries/documents"
import { useCreateStories } from "@/queries/stories"
import { BackToStories } from "./back-link"
import {
  REQUEST_LIMITS,
  defaultRequestValues,
  toAdHocInput,
  validateRequest,
  type RequestFormValues,
} from "./model"

const languageItems = LANGUAGES.map((l) => ({ value: l, label: m.values.language[l] }))
const refsFieldMap = { refs: "refs" } as const

export function NewRequestPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")

  if (!canEdit) {
    return (
      <div className="max-w-5xl">
        <BackToStories workspaceId={workspace.id} />
        <EmptyState title={m.request.forbidden} />
      </div>
    )
  }

  return (
    <div className="max-w-5xl">
      <BackToStories workspaceId={workspace.id} />
      <PageHeader title={m.request.title} description={m.request.description} />
      <RequestForm workspaceId={workspace.id} />
    </div>
  )
}

function RequestForm({ workspaceId }: { workspaceId: string }) {
  const create = useCreateStories(workspaceId)
  const navigate = useNavigate()
  const documents = useQuery(allDocumentsQuery(workspaceId))
  const known: KnownDocuments | undefined = useMemo(
    () => documents.data && new Map(documents.data.map((d) => [d.id.toLowerCase(), d])),
    [documents.data]
  )
  const [initial] = useState(defaultRequestValues)

  const form = useAppForm({
    defaultValues: initial,
    validators: {
      onChange: ({ value }) => {
        const r = validateRequest(value, known)
        if (!hasErrors(r)) return undefined
        return {
          form: r.form,
          fields: r.fields as Partial<Record<DeepKeys<RequestFormValues>, string>>,
        }
      },
    },
    // An invalid form's first submit returns before validating again, and
    // rows added since the last validation (document references) may have
    // lost their errors on remount; validate now so every error shows.
    onSubmitInvalid: ({ formApi }) => void formApi.validate("change"),
    onSubmit: async ({ value }) => {
      try {
        await create.mutateAsync(toAdHocInput(value))
      } catch (err) {
        toast.error(errorMessage(err))
        return
      }
      toast.success(m.request.queued, { description: m.request.queuedBody })
      void navigate({
        to: "/w/$workspaceId/stories",
        params: { workspaceId },
        search: { tab: "progress" },
      })
    },
  })

  const submitted = useStore(form.store, (s) => s.submissionAttempts > 0)
  const formError = useStore(form.store, (s) => {
    const e = s.errorMap.onChange
    return typeof e === "string" ? e : undefined
  })
  const language = useStore(form.store, (s) => s.values.language)
  const refCount = useStore(form.store, (s) => s.values.refs.length)
  const videoEnabled = useStore(form.store, (s) => s.values.videoEnabled)
  const articleEnabled = useStore(form.store, (s) => s.values.articleEnabled)

  // Re-check the references once the documents arrive (or change status).
  useEffect(() => {
    if (known && refCount > 0) void form.validate("change")
  }, [form, known, refCount])

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
      <Section title={m.request.sections.idea}>
        <FieldGroup className="gap-5">
          <form.Field name="idea">
            {(field) => {
              const length = [...field.state.value].length
              const error = shown(field.state.meta)
              return (
                <Row label={m.request.idea} htmlFor="rq-idea" error={error}>
                  <Textarea
                    id="rq-idea"
                    rows={8}
                        className="min-h-48 text-base leading-relaxed md:text-[15px]"
                    placeholder={m.request.ideaPlaceholder}
                    value={field.state.value}
                    onChange={(e) => field.handleChange(e.target.value)}
                    onBlur={field.handleBlur}
                    aria-invalid={!!error || undefined}
                    aria-describedby="rq-idea-count"
                  />
                  <div className="flex flex-wrap justify-between gap-2 text-sm text-muted-foreground">
                    {error ? null : <span>{m.request.ideaHint}</span>}
                    <span
                      id="rq-idea-count"
                      className={cn(
                        "ml-auto tabular",
                        length > REQUEST_LIMITS.idea && "font-medium text-destructive"
                      )}
                    >
                      {m.request.ideaCount(formatInteger(length), formatInteger(REQUEST_LIMITS.idea))}
                    </span>
                  </div>
                </Row>
              )
            }}
          </form.Field>
          <div className="grid gap-5 sm:grid-cols-[16rem_14rem]">
            <form.Field name="count">
              {(field) => (
                <Row
                  label={m.request.count}
                  htmlFor="rq-count"
                  hint={m.request.countHint}
                  error={shown(field.state.meta)}
                >
                  <NumberInput
                    id="rq-count"
                    value={field.state.value}
                    onChange={field.handleChange}
                    onBlur={field.handleBlur}
                    min={REQUEST_LIMITS.count[0]}
                    max={REQUEST_LIMITS.count[1]}
                    invalid={!!shown(field.state.meta)}
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
                <Row label={m.request.language} htmlFor="rq-language">
                  <Select
                    items={languageItems}
                    value={field.state.value}
                    onValueChange={(v) => v && field.handleChange(v as Language)}
                  >
                    <SelectTrigger id="rq-language" className="w-full">
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
          </div>
        </FieldGroup>
      </Section>

      <Section title={m.request.sections.outputs} description={m.request.outputsHint}>
        <OutputsFields
          form={form}
          fields={outputsFieldMap}
          language={language}
          readOnly={false}
          submitted={submitted}
          formError={formError}
          idPrefix="rq"
        />
      </Section>

      <form.Field name="voice">
        {(field) => (
          <VoiceOverrideSection
            idPrefix="rq-voice"
            language={language}
            value={field.state.value}
            onChange={field.handleChange}
          />
        )}
      </form.Field>

      <form.Field name="selfReview">
        {(field) => <SelfReviewSection value={field.state.value} onChange={field.handleChange} />}
      </form.Field>

      <form.Field name="production">
        {(field) => (
          <ProductionSection
            idPrefix="rq-production"
            value={field.state.value}
            onChange={field.handleChange}
          />
        )}
      </form.Field>

      <Section title={m.request.sections.extras} description={m.request.extrasHint}>
        <FieldGroup className="gap-6">
          <form.Field name="templateId">
            {(field) => (
              <TemplateSelect
                id="rq-template"
                value={field.state.value}
                onChange={field.handleChange}
                kinds={chosenKinds({ videoEnabled, articleEnabled })}
              />
            )}
          </form.Field>
          <form.Field name="style">
            {(field) => (
              <Row
                label={m.request.style}
                htmlFor="rq-style"
                hint={m.request.styleHint}
                error={shown(field.state.meta)}
              >
                <Textarea
                  id="rq-style"
                  rows={3}
                  className="min-h-20"
                  placeholder={m.request.stylePlaceholder}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={!!shown(field.state.meta) || undefined}
                />
              </Row>
            )}
          </form.Field>
          <RefsFields
            form={form}
            fields={refsFieldMap}
            workspaceId={workspaceId}
            label={m.request.refs}
            hint={m.request.refsHint}
            readOnly={false}
            submitted={submitted}
            idPrefix="rq-ref"
          />
        </FieldGroup>
      </Section>

      {create.error ? (
        <Banner title={m.request.serverError}>
          <p>{errorMessage(create.error)}</p>
        </Banner>
      ) : null}

      <div className="flex flex-wrap justify-end gap-2">
        <Button
          variant="outline"
          render={<Link to="/w/$workspaceId/stories" params={{ workspaceId }} />}
          nativeButton={false}
        >
          {m.common.cancel}
        </Button>
        <form.Subscribe selector={(s) => s.isSubmitting}>
          {(submitting) => (
            <Button type="submit" disabled={submitting}>
              {submitting ? m.request.submitting : m.request.submit}
            </Button>
          )}
        </form.Subscribe>
      </div>
    </form>
  )
}
