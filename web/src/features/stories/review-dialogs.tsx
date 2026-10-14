import { useForm } from "@tanstack/react-form"
import { toast } from "sonner"

import type { Deliverable, FeedbackScope, Story } from "@/api/client"
import { Banner } from "@/components/page"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
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
import { useRetained } from "@/lib/use-retained"
import { useDeleteStory, useDeliverableAction, useRequestChanges } from "@/queries/stories"
import { SCOPES, formatTimestamp, scopeLabel } from "./model"

/** The server's feedback limit (story/v1 MaxFeedbackRunes). */
const MAX_FEEDBACK = 5000

/**
 * Sends a deliverable in review back to the agent. For videos, `atSeconds`
 * is the player's position when the dialog opened; the reviewer can pin
 * the feedback to it.
 */
export function RequestChangesDialog({
  workspaceId,
  storyId,
  deliverable,
  atSeconds,
  onOpenChange,
}: {
  workspaceId: string
  storyId: string
  /** The deliverable to revise; null closes the dialog. */
  deliverable: Deliverable | null
  atSeconds?: number
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={!!deliverable} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        {/* Mounted only while open, so each opening starts empty. */}
        {deliverable ? (
          <ChangesForm
            workspaceId={workspaceId}
            storyId={storyId}
            deliverable={deliverable}
            atSeconds={atSeconds}
            onDone={() => onOpenChange(false)}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

const textError = (text: string) => {
  const t = text.trim()
  if (!t) return m.review.changes.textRequired
  if ([...t].length > MAX_FEEDBACK) return m.review.changes.textTooLong
  return undefined
}

function ChangesForm({
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

/** Confirms rejecting a deliverable; the server's refusal stays in the dialog. */
export function RejectDialog({
  workspaceId,
  storyId,
  deliverable,
  onOpenChange,
}: {
  workspaceId: string
  storyId: string
  deliverable: Deliverable | null
  onOpenChange: (open: boolean) => void
}) {
  const target = useRetained(deliverable)
  const reject = useDeliverableAction(workspaceId, storyId, "reject")
  const kind = (m.values.kind[target?.kind ?? "video"] ?? "").toLowerCase()

  const confirm = async () => {
    if (!target) return
    try {
      await reject.mutateAsync(target.id)
    } catch {
      return // shown in the dialog
    }
    toast.success(m.review.rejected)
    onOpenChange(false)
  }

  return (
    <AlertDialog
      open={!!deliverable}
      onOpenChange={(open) => {
        if (!open) reject.reset()
        onOpenChange(open)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.review.rejectTitle(kind)}</AlertDialogTitle>
          <AlertDialogDescription>{m.review.rejectBody}</AlertDialogDescription>
        </AlertDialogHeader>
        {reject.error ? <Banner title={errorMessage(reject.error)} /> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={reject.isPending}
            onClick={() => void confirm()}
          >
            {m.review.reject}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

/** Confirms deleting a story. While a step runs the server answers 409; that stays in the dialog. */
export function DeleteStoryDialog({
  workspaceId,
  story,
  onOpenChange,
  onDeleted,
}: {
  workspaceId: string
  story: Story | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const target = useRetained(story)
  const del = useDeleteStory(workspaceId)

  const confirm = async () => {
    if (!target) return
    try {
      await del.mutateAsync(target.id)
    } catch {
      return // shown in the dialog
    }
    toast.success(m.story.deleted(target.title))
    onOpenChange(false)
    onDeleted()
  }

  return (
    <AlertDialog
      open={!!story}
      onOpenChange={(open) => {
        if (!open) del.reset()
        onOpenChange(open)
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.story.deleteTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.story.deleteBody(target?.title ?? "")}</AlertDialogDescription>
        </AlertDialogHeader>
        {del.error ? <Banner title={errorMessage(del.error)} /> : null}
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={del.isPending}
            onClick={() => void confirm()}
          >
            {m.common.delete}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
