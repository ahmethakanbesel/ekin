import { useForm } from "@tanstack/react-form"
import { toast } from "sonner"

import type { Deliverable, FeedbackScope } from "@/api/client"
import { Banner } from "@/components/page"
import { Button } from "@/components/ui/button"
import {
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
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
import { errorMessage } from "@/lib/errors"
import { useRequestChanges } from "@/queries/stories"
import { SCOPES, formatTimestamp, scopeLabel } from "./model"

/** The server's feedback limit (story/v1 MaxFeedbackRunes). */
export const MAX_FEEDBACK = 5000

const textError = (text: string) => {
  const t = text.trim()
  if (!t) return m.review.changes.textRequired
  if ([...t].length > MAX_FEEDBACK) return m.review.changes.textTooLong
  return undefined
}

/**
 * Feedback for the agent on a deliverable in review: what should change,
 * what it is about and, for a video, where.
 */
export function FeedbackForm({
  workspaceId,
  storyId,
  deliverable: d,
  atSeconds,
  onDone,
}: {
  workspaceId: string
  storyId: string
  deliverable: Deliverable
  atSeconds?: number
  onDone: () => void
}) {
  const request = useRequestChanges(workspaceId, storyId)
  const canPin = d.kind === "video" && atSeconds !== undefined
  const scopes = SCOPES[d.kind]
  const scopeItems = scopes.map((s) => ({ value: s, label: scopeLabel(s, d.kind) }))
  const form = useForm({
    defaultValues: { text: "", scope: "all" as FeedbackScope, pinned: false },
    onSubmit: async ({ value }) => {
      try {
        await request.mutateAsync({
          deliverableId: d.id,
          body: {
            text: value.text.trim(),
            scope: value.scope,
            atSeconds:
              canPin && value.pinned ? Math.round((atSeconds ?? 0) * 10) / 10 : undefined,
          },
        })
      } catch {
        return // shown in the banner
      }
      toast.success(m.review.changes.sent)
      onDone()
    },
  })

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{m.review.changes.title}</DialogTitle>
        <DialogDescription>{m.review.changes.description(d.revision)}</DialogDescription>
      </DialogHeader>
      <form.Field
        name="text"
        validators={{
          onChange: ({ value }) => textError(value),
          onSubmit: ({ value }) => textError(value),
        }}
      >
        {(field) => {
          const invalid = field.state.meta.isTouched && !field.state.meta.isValid
          return (
            <Field data-invalid={invalid || undefined}>
              <FieldLabel htmlFor="changes-text">{m.review.changes.text}</FieldLabel>
              <Textarea
                id="changes-text"
                rows={5}
                className="min-h-28"
                placeholder={
                  d.kind === "video"
                    ? m.review.changes.placeholderVideo
                    : m.review.changes.placeholderArticle
                }
                value={field.state.value}
                onChange={(e) => field.handleChange(e.target.value)}
                onBlur={field.handleBlur}
                aria-invalid={invalid || undefined}
              />
              {invalid ? <FieldError>{String(field.state.meta.errors[0])}</FieldError> : null}
            </Field>
          )
        }}
      </form.Field>
      <form.Field name="scope">
        {(field) => (
          <Field>
            <FieldLabel htmlFor="changes-scope">{m.review.changes.scope}</FieldLabel>
            <Select
              items={scopeItems}
              value={field.state.value}
              onValueChange={(v) => v && field.handleChange(v as FeedbackScope)}
            >
              <SelectTrigger id="changes-scope" className="w-full sm:w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {scopeItems.map((it) => (
                  <SelectItem key={it.value} value={it.value}>
                    {it.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FieldDescription>{m.review.changes.scopeHint}</FieldDescription>
          </Field>
        )}
      </form.Field>
      {canPin ? (
        <form.Field name="pinned">
          {(field) => (
            <Field orientation="horizontal">
              <Switch
                id="changes-pin"
                aria-label={m.review.changes.pin(formatTimestamp(atSeconds ?? 0))}
                checked={field.state.value}
                onCheckedChange={(v) => field.handleChange(v)}
              />
              <FieldContent>
                <FieldLabel htmlFor="changes-pin" className="tabular">
                  {m.review.changes.pin(formatTimestamp(atSeconds ?? 0))}
                </FieldLabel>
                <FieldDescription>{m.review.changes.pinHint}</FieldDescription>
              </FieldContent>
            </Field>
          )}
        </form.Field>
      ) : null}
      {request.error ? <Banner title={errorMessage(request.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <form.Subscribe selector={(s) => s.isSubmitting}>
          {(submitting) => (
            <Button type="submit" disabled={submitting}>
              {submitting ? m.review.changes.sending : m.review.changes.submit}
            </Button>
          )}
        </form.Subscribe>
      </DialogFooter>
    </form>
  )
}
