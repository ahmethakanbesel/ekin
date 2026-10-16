import { toast } from "sonner"

import type { Deliverable, Story } from "@/api/client"
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
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useRetained } from "@/lib/use-retained"
import { useDeleteStory, useDeliverableAction } from "@/queries/stories"
import { FeedbackForm } from "./feedback-form"

/**
 * Sends a deliverable in review back to the agent. For videos, `atSeconds`
 * is the player's position when the dialog opened; the note is pinned to
 * it unless the reviewer clears the time.
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
          <>
            <DialogHeader>
              <DialogTitle>{m.review.changes.title}</DialogTitle>
              <DialogDescription>
                {m.review.changes.description(deliverable.revision)}
              </DialogDescription>
            </DialogHeader>
            <FeedbackForm
              workspaceId={workspaceId}
              storyId={storyId}
              deliverable={deliverable}
              revisions={[]}
              focus={{ at: atSeconds, n: 0 }}
              onSent={() => onOpenChange(false)}
              renderFooter={(submitting) => (
                <DialogFooter className="mt-6">
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => onOpenChange(false)}
                  >
                    {m.common.cancel}
                  </Button>
                  <Button type="submit" disabled={submitting}>
                    {submitting
                      ? m.review.changes.sending
                      : m.review.changes.submit}
                  </Button>
                </DialogFooter>
              )}
            />
          </>
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

/** Confirms rejecting a deliverable; the server's refusal stays in the dialog. */
export function RejectDialog({
  workspaceId,
  storyId,
  deliverable,
  onOpenChange,
  onRejected,
}: {
  workspaceId: string
  storyId: string
  deliverable: Deliverable | null
  onOpenChange: (open: boolean) => void
  /** After the server accepted it, with the updated story. */
  onRejected?: (story: Story) => void
}) {
  const target = useRetained(deliverable)
  const reject = useDeliverableAction(workspaceId, storyId, "reject")
  const kind = (m.values.kind[target?.kind ?? "video"] ?? "").toLowerCase()

  const confirm = async () => {
    if (!target) return
    let updated: Story
    try {
      updated = await reject.mutateAsync(target.id)
    } catch {
      return // shown in the dialog
    }
    toast.success(m.review.rejected)
    onOpenChange(false)
    onRejected?.(updated)
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
