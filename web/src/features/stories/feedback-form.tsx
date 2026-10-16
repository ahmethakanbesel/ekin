import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react"
import { useForm } from "@tanstack/react-form"
import { CrosshairIcon, PlusIcon, XIcon } from "lucide-react"
import { toast } from "sonner"

import type {
  Deliverable,
  FeedbackNote,
  FeedbackScope,
  Revision,
  Story,
} from "@/api/client"
import { Banner } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Field, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Kbd, KbdGroup } from "@/components/ui/kbd"
import { Textarea } from "@/components/ui/textarea"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { useMemberNames } from "@/queries/members"
import { useRequestChanges } from "@/queries/stories"
import { SCOPES, formatTimestamp, parseTimestamp, scopeLabel } from "./model"
import {
  EMPTY_NOTE,
  sortNotes,
  type FeedbackDraft,
  type NoteDraft,
  type PendingNote,
} from "./use-deliverable-review"

/** The server's feedback limit (story/v1 MaxFeedbackRunes). */
export const MAX_FEEDBACK = 5000
/** The most notes one request carries (story/v1 MaxFeedbackNotes). */
export const MAX_NOTES = 20

/** The note's text problem; an empty note is fine when `optional`. */
const textError = (text: string, optional = false) => {
  const t = text.trim()
  if (!t) return optional ? undefined : m.review.changes.textRequired
  if ([...t].length > MAX_FEEDBACK) return m.review.changes.textTooLong
  return undefined
}

const timeError = (raw: string) =>
  raw.trim() && parseTimestamp(raw) === undefined
    ? m.feedbackSheet.timestampInvalid
    : undefined

/** Seconds to one decimal, as the server keeps them. */
const tenths = (t: number) => Math.round(t * 10) / 10

/** Asks the open form to pin the note to a time and put the cursor in it. */
export type FeedbackFocus = { at?: number; n: number }

/** Where the unsent notes live between openings of the form. */
export type DraftStore = {
  get: () => FeedbackDraft
  saveCurrent: (v: NoteDraft) => void
  setNotes: (notes: PendingNote[]) => void
  clear: () => void
}

/** What the footer shows: the send button's state. */
export type FeedbackFooterState = {
  sending: boolean
  /** Notes Send would send, the one being written included. */
  count: number
}

const isMac =
  typeof navigator !== "undefined" &&
  /Mac|iPhone|iPad/.test(navigator.userAgent)

/** ⌘ Enter (Ctrl Enter elsewhere), the key that adds a note. */
export function AddKeys() {
  return (
    <KbdGroup>
      <Kbd>{isMac ? "⌘" : "Ctrl"}</Kbd>
      <Kbd>Enter</Kbd>
    </KbdGroup>
  )
}

/**
 * Feedback for the agent on a deliverable in review, written as a list of
 * notes: each says what it is about, where in the video, and what should
 * change. Add puts the note on the list; Send sends the list (and the
 * note being written) as one request. The fields scroll in
 * `bodyClassName`; `renderFooter` holds the send button.
 */
export function FeedbackForm({
  workspaceId,
  storyId,
  lang,
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
  /** The story's language, which notes are written in. */
  lang: string
  deliverable: Deliverable
  /** For the earlier notes on this deliverable. */
  revisions: Revision[]
  /** The player's position, when there is a player showing the latest revision. */
  playhead?: () => number | undefined
  focus?: FeedbackFocus | null
  /** Keeps the notes when the form unmounts; cleared once they are sent. */
  draft: DraftStore
  bodyClassName?: string
  renderFooter: (state: FeedbackFooterState) => ReactNode
  /** After the server took it, with the updated story. */
  onSent?: (story: Story) => void
}) {
  const request = useRequestChanges(workspaceId, storyId)
  const video = d.kind === "video"
  const noteRef = useRef<HTMLTextAreaElement>(null)
  // What a submit is for: adding the note to the list, or sending them all.
  const intent = useRef<"add" | "send">("send")
  // Said to screen readers after a note is added or removed.
  const [announced, setAnnounced] = useState("")
  const [tooMany, setTooMany] = useState(false)
  const pending = draft.get().notes

  /** The note being written as a list entry. */
  const toPending = (v: NoteDraft, notes: PendingNote[]): PendingNote => {
    const at = video ? parseTimestamp(v.at) : undefined
    return {
      id: Math.max(0, ...notes.map((n) => n.id)) + 1,
      text: v.text.trim(),
      scope: v.scope,
      at: at !== undefined ? tenths(at) : undefined,
    }
  }

  /** Starts the next note: empty, about everything, at no time. */
  const startOver = (next: NoteDraft = EMPTY_NOTE) => {
    form.reset(next, { keepDefaultValues: true })
    draft.saveCurrent(next)
  }

  /** Puts the note being written on the list. False when it cannot go yet. */
  const addNote = (v: NoteDraft): boolean => {
    const notes = draft.get().notes
    if (notes.length >= MAX_NOTES) {
      setTooMany(true)
      return false
    }
    const next = [...notes, toPending(v, notes)]
    draft.setNotes(next)
    startOver()
    setTooMany(false)
    setAnnounced(m.feedbackSheet.added(next.length))
    return true
  }

  const form = useForm({
    // A time asked for by `focus` is set once mounted (below).
    defaultValues: draft.get().current,
    listeners: {
      onChange: ({ formApi }) => draft.saveCurrent(formApi.state.values),
    },
    onSubmit: async ({ value }) => {
      if (intent.current === "add") {
        if (addNote(value))
          requestAnimationFrame(() => noteRef.current?.focus())
        return
      }
      const notes = [...draft.get().notes]
      if (value.text.trim()) notes.push(toPending(value, notes))
      if (notes.length > MAX_NOTES) {
        setTooMany(true)
        return
      }
      let updated: Story
      try {
        updated = await request.mutateAsync({
          deliverableId: d.id,
          body: {
            notes: sortNotes(notes).map((n) => ({
              text: n.text,
              scope: n.scope,
              atSeconds: n.at,
            })),
          },
        })
      } catch {
        return // shown in the banner
      }
      toast.success(m.review.changes.sent(notes.length))
      draft.clear()
      form.reset(EMPTY_NOTE)
      onSent?.(updated)
    },
  })

  const add = () => {
    intent.current = "add"
    void form.handleSubmit()
  }

  /** Loads a listed note back into the form; the note being written takes its place on the list. */
  const edit = (n: PendingNote) => {
    const cur = form.state.values
    let rest = draft.get().notes.filter((x) => x.id !== n.id)
    if (cur.text.trim()) {
      if (textError(cur.text) || (video && timeError(cur.at))) {
        add() // shows what is wrong with it
        return
      }
      rest = [...rest, toPending(cur, rest)]
    }
    draft.setNotes(rest)
    startOver({
      text: n.text,
      scope: n.scope,
      at: n.at !== undefined ? formatTimestamp(n.at) : "",
    })
    setTooMany(false)
    requestAnimationFrame(() => noteRef.current?.focus())
  }

  const remove = (n: PendingNote) => {
    const rest = draft.get().notes.filter((x) => x.id !== n.id)
    draft.setNotes(rest)
    setTooMany(false)
    setAnnounced(m.feedbackSheet.removed(rest.length))
    noteRef.current?.focus()
  }

  // Asked to focus (N, or Request changes on the rail): a new note at the
  // time the player paused. A note already written goes on the list first.
  const latest = useRef({ form, draft, video, addNote })
  useLayoutEffect(() => {
    latest.current = { form, draft, video, addNote }
  })
  const focusN = focus?.n
  const focusAt = focus?.at
  useEffect(() => {
    if (focusN === undefined) return
    const now = latest.current
    if (now.video && focusAt) {
      const at = formatTimestamp(focusAt)
      const cur = now.form.state.values
      const written =
        cur.text.trim() !== "" &&
        !textError(cur.text) &&
        !timeError(cur.at) &&
        cur.at.trim() !== at
      if (!written || now.addNote(cur)) {
        now.form.setFieldValue("at", at)
        now.draft.saveCurrent({ ...now.form.state.values, at })
      }
    }
    const id = requestAnimationFrame(() =>
      noteRef.current?.focus({ preventScroll: true })
    )
    return () => cancelAnimationFrame(id)
  }, [focusN, focusAt])

  const earlier = earlierNotes(d, revisions)
  const nameOf = useMemberNames(workspaceId, earlier.some((e) => e.by))

  return (
    <form
      noValidate
      className="flex min-h-0 flex-1 flex-col"
      onSubmit={(e) => {
        e.preventDefault()
        intent.current = "send"
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
            onChange: ({ value }) => textError(value, true),
            // Sending without a new note is fine once the list has some.
            onSubmit: ({ value }) =>
              textError(
                value,
                intent.current === "send" && draft.get().notes.length > 0
              ),
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
                  onKeyDown={(e) => {
                    if (e.key !== "Enter" || !(e.metaKey || e.ctrlKey)) return
                    e.preventDefault()
                    add()
                  }}
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
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    className="max-sm:w-full"
                    aria-keyshortcuts={isMac ? "Meta+Enter" : "Control+Enter"}
                    onClick={add}
                  >
                    <PlusIcon data-icon="inline-start" />
                    {m.feedbackSheet.add}
                    <span className="ml-1 pointer-coarse:hidden" aria-hidden="true">
                      <AddKeys />
                    </span>
                  </Button>
                </div>
              </Field>
            )
          }}
        </form.Field>
        {tooMany ? (
          <Banner title={m.feedbackSheet.tooMany(MAX_NOTES)} />
        ) : null}
        {pending.length > 0 ? (
          <PendingList
            notes={pending}
            kind={d.kind}
            lang={lang}
            onEdit={edit}
            onRemove={remove}
          />
        ) : null}
        <p className="sr-only" aria-live="polite">
          {announced}
        </p>
        {earlier.length > 0 ? (
          <div className="grid gap-3">
            <p className="text-sm leading-none font-medium">
              {m.feedbackSheet.earlier}
            </p>
            <ol className="grid gap-4 text-sm">
              {earlier.map((round) => (
                <li key={round.on} className="grid gap-1.5">
                  <p className="text-xs text-muted-foreground">
                    {round.addressedIn !== undefined
                      ? m.feedbackSheet.addressed(round.on, round.addressedIn)
                      : m.feedbackSheet.pending(round.on)}
                    {nameOf(round.by) ? ` · ${nameOf(round.by)}` : null}
                  </p>
                  <ul className="grid gap-1.5">
                    {round.notes.map((n, i) => (
                      <li key={i} className="flex gap-3">
                        <span className="tabular w-10 shrink-0 font-mono text-xs leading-5">
                          {n.atSeconds !== undefined
                            ? formatTimestamp(n.atSeconds)
                            : "—"}
                        </span>
                        <span className="min-w-0 text-muted-foreground">
                          <span className="text-xs">
                            {scopeLabel(n.scope, d.kind)}
                          </span>
                          <span
                            lang={lang}
                            className="mt-0.5 line-clamp-2 text-xs text-foreground/80"
                          >
                            “{n.text}”
                          </span>
                        </span>
                      </li>
                    ))}
                  </ul>
                </li>
              ))}
            </ol>
          </div>
        ) : null}
        {request.error ? <Banner title={errorMessage(request.error)} /> : null}
      </div>
      <form.Subscribe selector={(s) => s.values.text}>
        {(text) =>
          renderFooter({
            sending: request.isPending,
            count: pending.length + (text.trim() ? 1 : 0),
          })
        }
      </form.Subscribe>
    </form>
  )
}

/**
 * The notes waiting to be sent, in reading order. A note opens back in
 * the form to be changed; the note being written takes its place.
 */
function PendingList({
  notes,
  kind,
  lang,
  onEdit,
  onRemove,
}: {
  notes: PendingNote[]
  kind: Deliverable["kind"]
  lang: string
  onEdit: (n: PendingNote) => void
  onRemove: (n: PendingNote) => void
}) {
  const video = kind === "video"
  return (
    <section aria-labelledby="feedback-pending" className="grid gap-3">
      <h3
        id="feedback-pending"
        className="flex items-baseline justify-between gap-2 text-sm leading-none font-medium"
      >
        {m.feedbackSheet.pendingTitle}
        <span className="tabular text-xs font-normal text-muted-foreground">
          {m.feedbackSheet.pendingCount(notes.length)}
        </span>
      </h3>
      <ul className="divide-y overflow-hidden rounded-xl border">
        {notes.map((n) => {
          const time = n.at !== undefined ? formatTimestamp(n.at) : undefined
          const scope = scopeLabel(n.scope, kind)
          return (
            <li key={n.id} className="flex items-start">
              <button
                type="button"
                className="flex min-w-0 flex-1 gap-3 px-3 py-2.5 text-left text-sm outline-none hover:bg-muted/40 focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:ring-inset"
                aria-label={m.feedbackSheet.editNote(time, scope, n.text)}
                onClick={() => onEdit(n)}
              >
                {video ? (
                  <span className="tabular w-10 shrink-0 font-mono text-xs leading-5">
                    {time ?? "—"}
                  </span>
                ) : null}
                <span className="grid min-w-0 gap-0.5">
                  <span className="text-xs leading-5 text-muted-foreground">
                    {scope}
                  </span>
                  <span lang={lang} className="line-clamp-3 text-foreground/90">
                    {n.text}
                  </span>
                </span>
              </button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                className="m-1.5 shrink-0 text-muted-foreground"
                aria-label={m.feedbackSheet.removeNote(time, scope)}
                onClick={() => onRemove(n)}
              >
                <XIcon />
              </Button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

type EarlierRound = {
  /** The revision the notes were given on. */
  on: number
  addressedIn?: number
  notes: FeedbackNote[]
  /** Who wrote them (a user id). */
  by?: string
}

/** Feedback already given on the deliverable, a round per revision, newest first. */
function earlierNotes(d: Deliverable, revisions: Revision[]): EarlierRound[] {
  const out: EarlierRound[] = []
  for (const r of revisions) {
    if (!r.feedback?.notes.length) continue
    out.push({
      on: r.number - 1,
      addressedIn: r.number,
      notes: r.feedback.notes,
      by: r.feedback.by,
    })
  }
  if (d.pendingFeedback?.notes.length)
    out.push({
      on: d.revision,
      notes: d.pendingFeedback.notes,
      by: d.pendingFeedback.by,
    })
  return out.toReversed()
}
