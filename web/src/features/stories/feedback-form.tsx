import { useEffect, useLayoutEffect, useRef, type ReactNode } from "react"
import { useForm } from "@tanstack/react-form"
import { CrosshairIcon } from "lucide-react"
import { toast } from "sonner"

import type { Deliverable, FeedbackScope, Revision, Story } from "@/api/client"
import { Banner } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { useRequestChanges } from "@/queries/stories"
import { SCOPES, formatTimestamp, parseTimestamp, scopeLabel } from "./model"
import type { FeedbackDraft } from "./use-deliverable-review"

/** The server's feedback limit (story/v1 MaxFeedbackRunes). */
export const MAX_FEEDBACK = 5000

const textError = (text: string) => {
  const t = text.trim()
  if (!t) return m.review.changes.textRequired
  if ([...t].length > MAX_FEEDBACK) return m.review.changes.textTooLong
  return undefined
}

const timeError = (raw: string) =>
  raw.trim() && parseTimestamp(raw) === undefined
    ? m.feedbackSheet.timestampInvalid
    : undefined

/** Asks the open form to pin the note to a time and put the cursor in it. */
export type FeedbackFocus = { at?: number; n: number }

/** Where the unsent note lives between openings of the form. */
export type DraftStore = {
  get: () => FeedbackDraft | undefined
  save: (v: FeedbackDraft) => void
  clear: () => void
}

const EMPTY: FeedbackDraft = { text: "", scope: "all", at: "" }

/**
 * Feedback for the agent on a deliverable in review: what it is about,
 * where in the video, and what should change. The fields scroll in
 * `bodyClassName`; `renderFooter` holds the submit button.
 */
export function FeedbackForm({
  workspaceId,
  storyId,
  deliverable: d,
  revisions,
  playhead,
  focus,
  draft,
  bodyClassName,
  renderFooter,
  onSent,
}: {
  workspaceId: string
  storyId: string
  deliverable: Deliverable
  /** For the earlier notes on this deliverable. */
  revisions: Revision[]
  /** The player's position, when there is a player showing the latest revision. */
  playhead?: () => number | undefined
  focus?: FeedbackFocus | null
  /** Keeps the note when the form unmounts; cleared once it is sent. */
  draft?: DraftStore
  bodyClassName?: string
  renderFooter: (submitting: boolean) => ReactNode
  /** After the server took it, with the updated story. */
  onSent?: (story: Story) => void
}) {
  const request = useRequestChanges(workspaceId, storyId)
  const video = d.kind === "video"
  const noteRef = useRef<HTMLTextAreaElement>(null)
  const form = useForm({
    defaultValues: {
      ...(draft?.get() ?? EMPTY),
      ...(focus?.at ? { at: formatTimestamp(focus.at) } : {}),
    },
    listeners: {
      onChange: ({ formApi }) => draft?.save(formApi.state.values),
    },
    onSubmit: async ({ value }) => {
      const at = video ? parseTimestamp(value.at) : undefined
      let updated: Story
      try {
        updated = await request.mutateAsync({
          deliverableId: d.id,
          body: {
            text: value.text.trim(),
            scope: value.scope,
            atSeconds: at !== undefined ? Math.round(at * 10) / 10 : undefined,
          },
        })
      } catch {
        return // shown in the banner
      }
      toast.success(m.review.changes.sent)
      draft?.clear()
      form.reset(EMPTY)
      onSent?.(updated)
    },
  })

  // Asked to focus (Request changes on the rail): pin the time it paused at.
  const setAt = form.setFieldValue
  const draftRef = useRef(draft)
  useLayoutEffect(() => {
    draftRef.current = draft
  })
  const focusN = focus?.n
  const focusAt = focus?.at
  useEffect(() => {
    if (focusN === undefined) return
    if (video && focusAt) {
      const at = formatTimestamp(focusAt)
      setAt("at", at)
      const store = draftRef.current
      store?.save({ ...(store.get() ?? EMPTY), at })
    }
    const id = requestAnimationFrame(() =>
      noteRef.current?.focus({ preventScroll: true })
    )
    return () => cancelAnimationFrame(id)
  }, [focusN, focusAt, video, setAt])

  const earlier = earlierNotes(d, revisions)

  return (
    <form
      noValidate
      className="flex min-h-0 flex-1 flex-col"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      <div className={cn("grid content-start gap-6", bodyClassName)}>
        <form.Field name="scope">
          {(field) => (
            <Field>
              <FieldLabel id="feedback-scope">
                {m.review.changes.scope}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby="feedback-scope"
                variant="outline"
                size="sm"
                className="flex-wrap"
                value={[field.state.value]}
                onValueChange={(next) =>
                  next[0] && field.handleChange(next[0] as FeedbackScope)
                }
              >
                {SCOPES[d.kind].map((s) => (
                  <ToggleGroupItem key={s} value={s}>
                    {scopeLabel(s, d.kind)}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>
          )}
        </form.Field>
        {video ? (
          <form.Field
            name="at"
            validators={{
              onChange: ({ value }) => timeError(value),
              onSubmit: ({ value }) => timeError(value),
            }}
          >
            {(field) => {
              const invalid =
                field.state.meta.isTouched && !field.state.meta.isValid
              return (
                <Field data-invalid={invalid || undefined}>
                  <FieldLabel htmlFor="feedback-at">
                    {m.feedbackSheet.timestamp}
                  </FieldLabel>
                  <div className="flex gap-2">
                    <Input
                      id="feedback-at"
                      inputMode="numeric"
                      autoComplete="off"
                      placeholder={m.feedbackSheet.timestampPlaceholder}
                      className="tabular w-24 font-mono"
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onBlur={field.handleBlur}
                      aria-invalid={invalid || undefined}
                      aria-describedby="feedback-at-hint"
                    />
                    <Button
                      type="button"
                      variant="outline"
                      disabled={!playhead}
                      onClick={() => {
                        const t = playhead?.()
                        if (t !== undefined)
                          field.handleChange(formatTimestamp(t))
                      }}
                    >
                      <CrosshairIcon data-icon="inline-start" />
                      {m.feedbackSheet.usePlayhead}
                    </Button>
                  </div>
                  {invalid ? (
                    <FieldError>{String(field.state.meta.errors[0])}</FieldError>
                  ) : (
                    <p
                      id="feedback-at-hint"
                      className="text-xs text-muted-foreground"
                    >
                      {m.feedbackSheet.timestampHint}
                    </p>
                  )}
                </Field>
              )
            }}
          </form.Field>
        ) : null}
        <form.Field
          name="text"
          validators={{
            onChange: ({ value }) => textError(value),
            onSubmit: ({ value }) => textError(value),
          }}
        >
          {(field) => {
            const invalid =
              field.state.meta.isTouched && !field.state.meta.isValid
            return (
              <Field data-invalid={invalid || undefined}>
                <FieldLabel htmlFor="feedback-text">
                  {m.feedbackSheet.note}
                </FieldLabel>
                <Textarea
                  ref={noteRef}
                  id="feedback-text"
                  rows={5}
                  className="min-h-28"
                  placeholder={
                    video
                      ? m.review.changes.placeholderVideo
                      : m.review.changes.placeholderArticle
                  }
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={invalid || undefined}
                  aria-describedby="feedback-text-count"
                />
                <div className="flex justify-between gap-3 text-xs text-muted-foreground">
                  {invalid ? (
                    <FieldError>{String(field.state.meta.errors[0])}</FieldError>
                  ) : (
                    <form.Subscribe selector={(s) => [s.values.scope, s.values.at]}>
                      {([scope, at]) => (
                        <span>
                          {scopeLabel(scope as FeedbackScope, d.kind)}
                          {video && parseTimestamp(at) !== undefined ? (
                            <>
                              {" · "}
                              <span className="font-mono">{at.trim()}</span>
                            </>
                          ) : null}
                        </span>
                      )}
                    </form.Subscribe>
                  )}
                  <span id="feedback-text-count" className="tabular shrink-0">
                    {m.feedbackSheet.count(
                      [...field.state.value].length,
                      MAX_FEEDBACK
                    )}
                  </span>
                </div>
              </Field>
            )
          }}
        </form.Field>
        {earlier.length > 0 ? (
          <div className="grid gap-3">
            <p className="text-sm leading-none font-medium">
              {m.feedbackSheet.earlier}
            </p>
            <ul className="grid gap-2 text-sm text-muted-foreground">
              {earlier.map((e) => (
                <li key={`${e.on}-${e.text}`} className="flex gap-3">
                  <span className="tabular w-10 shrink-0 font-mono text-foreground">
                    {e.at !== undefined ? formatTimestamp(e.at) : "—"}
                  </span>
                  <span className="min-w-0">
                    {e.addressedIn !== undefined
                      ? m.feedbackSheet.addressed(e.on, e.scope, e.addressedIn)
                      : m.feedbackSheet.pending(e.on, e.scope)}
                    <span className="mt-0.5 line-clamp-2 text-xs text-foreground/80">
                      “{e.text}”
                    </span>
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        {request.error ? <Banner title={errorMessage(request.error)} /> : null}
      </div>
      <form.Subscribe selector={(s) => s.isSubmitting}>
        {(submitting) => renderFooter(submitting)}
      </form.Subscribe>
    </form>
  )
}

type EarlierNote = {
  /** The revision the note was given on. */
  on: number
  addressedIn?: number
  scope: string
  at?: number
  text: string
}

/** Feedback already given on the deliverable, newest first. */
function earlierNotes(d: Deliverable, revisions: Revision[]): EarlierNote[] {
  const out: EarlierNote[] = []
  for (const r of revisions) {
    if (!r.feedback) continue
    out.push({
      on: r.number - 1,
      addressedIn: r.number,
      scope: scopeLabel(r.feedback.scope, d.kind),
      at: r.feedback.atSeconds,
      text: r.feedback.text,
    })
  }
  if (d.pendingFeedback)
    out.push({
      on: d.revision,
      scope: scopeLabel(d.pendingFeedback.scope, d.kind),
      at: d.pendingFeedback.atSeconds,
      text: d.pendingFeedback.text,
    })
  return out.toReversed()
}
