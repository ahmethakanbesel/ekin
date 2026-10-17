import type { QueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import type { Deliverable, Story } from "@/api/client"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { approveDeliverable } from "@/queries/stories"
import {
  APPROVE_DELAY_MS,
  cancelApproval,
  scheduleApproval,
} from "./pending-approvals"
import { forgetDecision, recordDecision } from "./sitting-ledger"

/**
 * Approves a deliverable a few seconds from now, with a toast that can undo
 * it. The approval is sent when the countdown ends (or the tab closes),
 * wherever the reviewer has moved on to by then. False when an approval of
 * it is already under way.
 */
export function startApproval({
  qc,
  workspaceId,
  story,
  deliverable: d,
  onUndo,
}: {
  qc: QueryClient
  workspaceId: string
  story: Story
  deliverable: Deliverable
  /** After the toast's Undo: take the reviewer back to the draft. */
  onUndo: () => void
}): boolean {
  const id = `approve-${d.id}`
  const n = d.revision
  const kind = m.values.kind[d.kind] ?? d.kind
  const what = <StoryRef story={story} kind={kind} />

  const scheduled = scheduleApproval(
    d.id,
    async (keepalive) => {
      try {
        await approveDeliverable(
          qc,
          { workspaceId, storyId: story.id, deliverableId: d.id },
          keepalive
        )
        toast.success(
          d.stage === "script"
            ? m.review.approvedScript
            : d.kind === "video"
              ? m.review.approvedVideo
              : m.review.approved,
          { id, description: what, action: undefined, duration: 4_000 }
        )
      } catch (err) {
        forgetDecision(d.id, n)
        toast.error(errorMessage(err), {
          id,
          description: what,
          action: undefined,
          duration: 8_000,
        })
      }
    },
    () => {
      forgetDecision(d.id, n)
      toast.dismiss(id)
    }
  )
  if (!scheduled) return false
  recordDecision(story.id, d.id, n, { kind: "approved" })

  toast(m.review.approving(n), {
    id,
    duration: Infinity,
    description: (
      <span className="grid gap-2">
        {what}
        <span>{m.review.approvingHint}</span>
        <Countdown />
      </span>
    ),
    action: {
      label: m.review.undo,
      onClick: () => {
        if (!cancelApproval(d.id)) return
        toast(m.review.approveUndone(n))
        onUndo()
      },
    },
  })
  return true
}

function StoryRef({ story, kind }: { story: Story; kind: string }) {
  return (
    <span>
      <span lang={story.language}>“{story.title}”</span> · {kind}
    </span>
  )
}

/** A bar draining over the undo window; hidden when motion is reduced. */
function Countdown() {
  return (
    <span
      className="block h-1 overflow-hidden rounded-full bg-muted motion-reduce:hidden"
      aria-hidden="true"
    >
      <span
        className="countdown block h-full rounded-full bg-success"
        style={{ animationDuration: `${APPROVE_DELAY_MS}ms` }}
      />
    </span>
  )
}
