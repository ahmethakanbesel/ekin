import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { Deliverable, Integrations, Story } from "@/api/client"
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
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useRetained } from "@/lib/use-retained"
import { integrationsQuery } from "@/queries/integrations"
import { useDeleteStory, useDeliverableAction } from "@/queries/stories"

/** Confirms rejecting a deliverable; the server's refusal stays in the dialog. */
export function RejectDialog({
  workspaceId,
  storyId,
  deliverable,
  unsent = 0,
  onOpenChange,
  onRejected,
}: {
  workspaceId: string
  storyId: string
  deliverable: Deliverable | null
  /** Notes written for it and not sent; rejecting discards them. */
  unsent?: number
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
          <AlertDialogDescription>
            {m.review.rejectBody}
            {unsent > 0 ? ` ${m.review.discardsNotes(unsent)}` : null}
          </AlertDialogDescription>
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

/**
 * Asks before approving a draft that has notes written for it and not
 * sent: approving closes it without changes, so the agent never reads them.
 */
export function ApproveWithNotesDialog({
  count,
  revision,
  onOpenChange,
  onApprove,
}: {
  /** The unsent notes; 0 closes the dialog. */
  count: number
  revision: number
  onOpenChange: (open: boolean) => void
  onApprove: () => void
}) {
  // Kept while the dialog animates closed.
  const kept = useRetained(count > 0 ? count : null) ?? 1
  return (
    <AlertDialog open={count > 0} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {m.review.approveWithNotesTitle(kept)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {m.review.approveWithNotesBody(revision, kept)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.review.keepNotes}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              onOpenChange(false)
              onApprove()
            }}
          >
            {m.review.approveAnyway}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

/**
 * Confirms publishing a deliverable, naming where it goes as far as the
 * workspace's publishing settings tell.
 */
export function PublishDialog({
  workspaceId,
  deliverable,
  busy,
  onOpenChange,
  onConfirm,
}: {
  workspaceId: string
  deliverable: Deliverable | null
  busy: boolean
  onOpenChange: (open: boolean) => void
  /** Publishes; a failure is shown on the page (and toasted). */
  onConfirm: () => Promise<unknown>
}) {
  const target = useRetained(deliverable)
  const integrations = useQuery({
    ...integrationsQuery(workspaceId),
    enabled: !!deliverable,
  })
  const kind = (m.values.kind[target?.kind ?? "video"] ?? "").toLowerCase()
  const where = destination(target?.kind ?? "video", integrations.data)

  const confirm = async () => {
    await onConfirm()
    onOpenChange(false)
  }

  return (
    <AlertDialog open={!!deliverable} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.review.publishTitle(kind)}</AlertDialogTitle>
          <AlertDialogDescription>
            {where.text}
            {where.ready === false ? null : ` ${m.review.publishUndoable}`}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction disabled={busy} onClick={() => void confirm()}>
            {m.review.publish}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

/**
 * Where a deliverable of this kind is published, in a sentence or two;
 * ready is false when the settings say there is nowhere to publish yet
 * (undefined while they load).
 */
function destination(
  kind: Deliverable["kind"],
  settings: Integrations | undefined
): { text: string; ready?: boolean } {
  const t = m.review
  if (kind === "video") {
    const yt = settings?.youtube
    if (!yt) return { text: t.publishYouTube }
    if (!yt.connected) return { text: t.publishNoChannel, ready: false }
    const privacy = yt.privacy ?? "private"
    const channel = yt.channelTitle
      ? t.publishChannel(
          yt.channelTitle,
          (m.values.privacy[privacy] ?? privacy).toLowerCase()
        )
      : t.publishYouTube
    return {
      text:
        privacy === "private" ? channel : `${channel} ${t.publishStaysPrivate}`,
      ready: true,
    }
  }
  if (!settings) return { text: t.publishBlog }
  const blog = settings.blog
  if (!blog) return { text: t.publishNoBlog, ready: false }
  const parts = [
    blog.kind === "github" && blog.repo
      ? t.publishGitHub(blog.repo, blog.branch || "main")
      : blog.kind === "dir" && blog.dir
        ? t.publishDir(blog.dir)
        : t.publishBlog,
  ]
  if (blog.draft) parts.push(t.publishDraft)
  const site = siteName(blog.siteUrl)
  if (site) parts.push(t.publishSite(site))
  return { text: parts.join(" "), ready: true }
}

function siteName(url: string): string {
  try {
    return url ? new URL(url).host : ""
  } catch {
    return ""
  }
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
