import { useEffect, useReducer, useRef, useState } from "react"
import {
  keepPreviousData,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { toast } from "sonner"

import type { Deliverable, FeedbackScope, Story } from "@/api/client"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { revisionsQuery, useDeliverableAction } from "@/queries/stories"
import { startApproval } from "./approval-toast"
import { cancelApproval, usePendingApproval } from "./pending-approvals"
import { createPlayback } from "./player"
import type { ArtifactRef } from "./previews"

/** A note for the agent not sent yet, as typed in the feedback form. */
export type FeedbackDraft = { text: string; scope: FeedbackScope; at: string }

/**
 * The state of reviewing one deliverable: its revisions and the one being
 * looked at, the player, and the approve / retry / publish actions with the
 * reject and edit flows they open.
 */
export function useDeliverableReview({
  workspaceId,
  story,
  deliverable: d,
  canEdit,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  canEdit: boolean
}) {
  const revisions = useQuery({
    ...revisionsQuery(workspaceId, story.id, d),
    enabled: d.revision > 0,
    // Keep showing the previous list while a new revision loads.
    placeholderData: keepPreviousData,
  })
  // null: follow the latest revision.
  const [picked, setPicked] = useState<number | null>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  // The player's state, for the scrubber and the script beside it.
  const [playback] = useState(createPlayback)
  const [rejecting, setRejecting] = useState(false)
  const [confirmingPublish, setConfirmingPublish] = useState(false)
  const [editing, setEditing] = useState(false)
  // Another story or deliverable starts afresh (the feed keeps this mounted).
  const key = `${story.id}/${d.id}`
  const [shown, setShown] = useState(key)
  if (shown !== key) {
    setShown(key)
    setPicked(null)
    setRejecting(false)
    setConfirmingPublish(false)
    setEditing(false)
  }
  // Unsent notes per draft (deliverable and revision). They outlive the
  // feedback sheet, which unmounts its form when it closes. The map is
  // written in place; only a new timestamp re-renders (the script marks it).
  const [drafts] = useState(() => new Map<string, FeedbackDraft>())
  const [, draftPinned] = useReducer((n: number) => n + 1, 0)
  const draftKey = `${d.id}/${d.revision}`
  // Editing applies to the latest revision of a deliverable in review only.
  const showEditor =
    editing && canEdit && d.status === "review" && d.revision > 0
  const qc = useQueryClient()
  // An approval waits a few seconds before it is sent, so it can be undone.
  const pendingApproval = usePendingApproval(d.id)
  const retry = useDeliverableAction(workspaceId, story.id, "retry")
  const publish = useDeliverableAction(workspaceId, story.id, "publish")

  const list = revisions.data ?? []
  const latest = list.at(-1)
  const current =
    (picked !== null ? list.find((r) => r.number === picked) : undefined) ??
    latest
  const viewingOld = !!current && !!latest && current.number !== latest.number

  // A publish error belongs to the deliverable it was about.
  const resetPublish = publish.reset
  const publishedFor = useRef(key)
  useEffect(() => {
    if (publishedFor.current === key) return
    publishedFor.current = key
    resetPublish()
  }, [key, resetPublish])

  /** Runs an action; the story it returns, or null when it failed (toasted). */
  const run = async (
    action: { mutateAsync: (id: string) => Promise<Story> },
    success: string
  ): Promise<Story | null> => {
    try {
      const updated = await action.mutateAsync(d.id)
      toast.success(success)
      return updated
    } catch (err) {
      toast.error(errorMessage(err))
      return null
    }
  }

  /** Where the player is now, for a video showing its latest revision. */
  const playhead = (): number | undefined => {
    const el = videoRef.current
    if (d.kind !== "video" || !el || viewingOld) return undefined
    return el.currentTime
  }

  /** Pauses the player where the reviewer is; that position, for pinning a note. */
  const pause = (): number | undefined => {
    const el = videoRef.current
    if (d.kind !== "video" || !el || viewingOld) return undefined
    el.pause()
    return el.currentTime
  }

  // A waiting approval holds the other decisions on this deliverable.
  const busy = !!pendingApproval || retry.isPending || publish.isPending

  const artifacts: ArtifactRef | undefined = current && {
    workspaceId,
    storyId: story.id,
    deliverableId: d.id,
    revision: current.number,
  }

  return {
    revisions,
    list,
    latest,
    current,
    viewingOld,
    artifacts,
    /** Show revision n; the latest one means "follow the latest". */
    pick: (n: number | null) =>
      setPicked(n === null || n === latest?.number ? null : n),
    /** The unsent note on the revision in review, kept while the sheet is closed. */
    draft: {
      get: () => drafts.get(draftKey),
      save: (v: FeedbackDraft) => {
        const pinnedBefore = drafts.get(draftKey)?.at ?? ""
        drafts.set(draftKey, v)
        if (v.at !== pinnedBefore) draftPinned()
      },
      clear: () => {
        if (drafts.delete(draftKey)) draftPinned()
      },
    },
    videoRef,
    playback,
    playhead,
    pause,
    rejecting,
    setRejecting,
    /** The publish confirmation is open. */
    confirmingPublish,
    setConfirmingPublish,
    editing: showEditor,
    setEditing,
    /**
     * Starts approving the deliverable: sent after a few seconds unless
     * undone. False when an approval of it is already under way.
     */
    approve: (onUndo: () => void) =>
      !pendingApproval &&
      startApproval({ qc, workspaceId, story, deliverable: d, onUndo }),
    pendingApproval,
    undoApproval: () => cancelApproval(d.id),
    retry: () => run(retry, m.review.retried),
    publish: () => run(publish, m.review.publishQueued),
    publishError: publish.error,
    busy,
  }
}

export type DeliverableReview = ReturnType<typeof useDeliverableReview>
