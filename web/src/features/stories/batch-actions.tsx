import type { QueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import type { Deliverable, Story } from "@/api/client"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { approveDeliverable, rejectDeliverable } from "@/queries/stories"
import {
  APPROVE_DELAY_MS,
  cancelApproval,
  scheduleApproval,
} from "./pending-approvals"
import { forgetDecision, recordDecision } from "./sitting-ledger"

/** One draft a batch decides: a deliverable waiting in review, and its story. */
export type BatchTarget = { story: Story; deliverable: Deliverable }

/** The drafts of the given stories that wait for a decision. */
export function reviewTargets(stories: Story[]): BatchTarget[] {
  return stories.flatMap((story) =>
    story.deliverables
      .filter((d) => d.status === "review")
      .map((deliverable) => ({ story, deliverable }))
  )
}

/** Whether a story has a draft a batch can decide. */
export const decidable = (s: Story) =>
  s.deliverables.some((d) => d.status === "review")

/** Marks an approval undone before it was sent. */
const UNDONE = Symbol("undone")

/** How many requests a batch sends at once. */
const PARALLEL = 4

/** Runs fn over items, PARALLEL at a time, and settles every one. */
async function settleAll<T>(
  items: T[],
  fn: (item: T) => Promise<unknown>
): Promise<PromiseSettledResult<unknown>[]> {
  const out: PromiseSettledResult<unknown>[] = Array.from({ length: items.length })
  let next = 0
  const worker = async () => {
    while (next < items.length) {
      const i = next++
      try {
        out[i] = { status: "fulfilled", value: await fn(items[i]) }
      } catch (reason) {
        out[i] = { status: "rejected", reason }
      }
    }
  }
  await Promise.all(
    Array.from({ length: Math.min(PARALLEL, items.length) }, worker)
  )
  return out
}

/** Says how a batch went: every draft, or how many did and the first error. */
function summarize(
  results: PromiseSettledResult<unknown>[],
  done: (n: number) => string,
  partly: (done: number, failed: number) => string,
  id: string
) {
  const failed = results.filter(
    (r): r is PromiseRejectedResult => r.status === "rejected"
  )
  const ok = results.length - failed.length
  if (failed.length === 0) {
    toast.success(done(ok), {
      id,
      description: undefined,
      action: undefined,
      duration: 4_000,
    })
    return
  }
  toast.error(partly(ok, failed.length), {
    id,
    description: errorMessage(failed[0].reason),
    action: undefined,
    duration: 10_000,
  })
}

/**
 * Approves every target a few seconds from now, with one toast that undoes
 * them all, as a single approval does. Targets already being approved are
 * left out. Returns how many were scheduled.
 */
export function startBatchApproval({
  qc,
  workspaceId,
  targets,
  onUndo,
}: {
  qc: QueryClient
  workspaceId: string
  targets: BatchTarget[]
  /** After Undo, with the targets that stay in review. */
  onUndo: (undone: BatchTarget[]) => void
}): number {
  const id = `approve-batch-${Date.now()}`
  const scheduled: BatchTarget[] = []
  const results: Promise<PromiseSettledResult<unknown>>[] = []
  for (const t of targets) {
    const { story, deliverable: d } = t
    let settle!: (r: PromiseSettledResult<unknown>) => void
    const result = new Promise<PromiseSettledResult<unknown>>(
      (resolve) => (settle = resolve)
    )
    const ok = scheduleApproval(
      d.id,
      async (keepalive) => {
        try {
          const value = await approveDeliverable(
            qc,
            { workspaceId, storyId: story.id, deliverableId: d.id },
            keepalive
          )
          settle({ status: "fulfilled", value })
        } catch (reason) {
          forgetDecision(d.id, d.revision)
          settle({ status: "rejected", reason })
        }
      },
      () => {
        forgetDecision(d.id, d.revision)
        settle({ status: "fulfilled", value: UNDONE })
      }
    )
    if (!ok) continue
    recordDecision(story.id, d.id, d.revision, { kind: "approved" })
    scheduled.push(t)
    results.push(result)
  }
  if (scheduled.length === 0) return 0

  void Promise.all(results).then((settled) => {
    const sent = settled.filter(
      (r) => !(r.status === "fulfilled" && r.value === UNDONE)
    )
    if (sent.length > 0)
      summarize(
        sent,
        m.stories.batch.approved,
        m.stories.batch.approvedPartly,
        id
      )
  })
  toast(m.stories.batch.approving(scheduled.length), {
    id,
    duration: Infinity,
    description: (
      <span className="grid gap-2">
        <span>{m.review.approvingHint}</span>
        <span
          className="block h-1 overflow-hidden rounded-full bg-muted motion-reduce:hidden"
          aria-hidden="true"
        >
          <span
            className="countdown block h-full rounded-full bg-success"
            style={{ animationDuration: `${APPROVE_DELAY_MS}ms` }}
          />
        </span>
      </span>
    ),
    action: {
      label: m.review.undo,
      onClick: () => {
        const back = scheduled.filter((t) => cancelApproval(t.deliverable.id))
        if (back.length === 0) return
        toast(m.stories.batch.undone(back.length), {
          id,
          description: undefined,
          action: undefined,
          duration: 4_000,
        })
        onUndo(back)
      },
    },
  })
  return scheduled.length
}

/** Rejects every target and says how it went. */
export async function rejectAll({
  qc,
  workspaceId,
  targets,
}: {
  qc: QueryClient
  workspaceId: string
  targets: BatchTarget[]
}): Promise<void> {
  const results = await settleAll(
    targets,
    async ({ story, deliverable: d }) => {
      await rejectDeliverable(qc, {
        workspaceId,
        storyId: story.id,
        deliverableId: d.id,
      })
      recordDecision(story.id, d.id, d.revision, { kind: "rejected" })
    }
  )
  summarize(
    results,
    m.stories.batch.rejected,
    m.stories.batch.rejectedPartly,
    `reject-batch-${Date.now()}`
  )
}
