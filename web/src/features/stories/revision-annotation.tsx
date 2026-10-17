import { useId, useRef, useState } from "react"
import { PencilIcon, StickyNoteIcon } from "lucide-react"

import type { Revision } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative } from "@/lib/time"
import { useAnnotateRevision } from "@/queries/stories"

/** The longest note the server keeps, in characters. */
const MAX_NOTE = 4000

/**
 * The one team note on a revision: shown when it has one, written or
 * changed in place by an editor. Saving blank text removes it.
 */
export function RevisionAnnotation({
  workspaceId,
  storyId,
  deliverableId,
  revision: r,
  canEdit,
  nameOf,
  now,
  timezone,
  lang,
}: {
  workspaceId: string
  storyId: string
  deliverableId: string
  revision: Revision
  canEdit: boolean
  nameOf: (id: string | undefined) => string | undefined
  now: number
  timezone: string
  /** The story's language, which the team often writes notes in. */
  lang: string
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const annotate = useAnnotateRevision(workspaceId, storyId, deliverableId)
  const fieldId = useId()
  const note = r.annotation
  const fieldRef = useRef<HTMLTextAreaElement>(null)
  // The add or edit button, which takes focus back when the editor closes.
  const triggerRef = useRef<HTMLButtonElement>(null)

  const open = (text: string) => {
    setDraft(text)
    requestAnimationFrame(() => fieldRef.current?.focus())
  }
  const close = () => {
    setDraft(null)
    annotate.reset()
    requestAnimationFrame(() => triggerRef.current?.focus())
  }
  const save = (text: string) =>
    annotate.mutate({ number: r.number, text }, { onSuccess: close })

  if (draft !== null) {
    const length = [...draft.trim()].length
    const tooLong = length > MAX_NOTE
    return (
      <form
        className="mt-2 grid gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          if (!tooLong) save(draft)
        }}
      >
        <label
          htmlFor={fieldId}
          className="text-xs font-medium text-muted-foreground"
        >
          {m.annotation.label}
          <span className="font-normal"> · {m.annotation.hint}</span>
        </label>
        <Textarea
          ref={fieldRef}
          id={fieldId}
          lang={lang}
          rows={3}
          value={draft}
          placeholder={m.annotation.placeholder}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault()
              close()
            } else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              if (!tooLong) save(draft)
            }
          }}
          aria-invalid={tooLong || !!annotate.error || undefined}
          disabled={annotate.isPending}
        />
        {annotate.error ? (
          <p className="text-sm text-destructive" role="alert">
            {errorMessage(annotate.error)}
          </p>
        ) : null}
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="submit"
            size="sm"
            disabled={tooLong || annotate.isPending}
          >
            {m.annotation.save}
          </Button>
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={close}
            disabled={annotate.isPending}
          >
            {m.annotation.cancel}
          </Button>
          {note ? (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="text-destructive hover:text-destructive"
              onClick={() => save("")}
              disabled={annotate.isPending}
            >
              {m.annotation.remove}
            </Button>
          ) : null}
          <span
            className={`tabular ml-auto text-xs ${tooLong ? "text-destructive" : "text-muted-foreground"}`}
            aria-live="polite"
          >
            {m.annotation.count(length, MAX_NOTE)}
          </span>
        </div>
      </form>
    )
  }

  if (!note) {
    return canEdit ? (
      <Button
        type="button"
        size="xs"
        variant="ghost"
        ref={triggerRef}
        className="mt-1.5 -ml-2 text-muted-foreground"
        onClick={() => open("")}
      >
        <StickyNoteIcon data-icon="inline-start" />
        {m.annotation.add}
      </Button>
    ) : null
  }

  return (
    <div className="mt-2 grid gap-1 rounded-lg border border-dashed px-3 py-2.5 text-sm">
      <div className="flex items-center gap-2 text-xs text-muted-foreground">
        <StickyNoteIcon className="size-3.5" aria-hidden="true" />
        <span className="font-medium">{m.annotation.label}</span>
        <span title={formatDateTime(note.at, timezone)}>
          {m.annotation.by(nameOf(note.by), formatRelative(note.at, now))}
        </span>
        {canEdit ? (
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            className="ml-auto"
            aria-label={m.annotation.edit}
            title={m.annotation.edit}
            ref={triggerRef}
            onClick={() => open(note.text)}
          >
            <PencilIcon />
          </Button>
        ) : null}
      </div>
      <p lang={lang} className="whitespace-pre-wrap">
        {note.text}
      </p>
    </div>
  )
}
