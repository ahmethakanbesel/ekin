import { Link } from "@tanstack/react-router"
import { CheckIcon, PencilLineIcon, XIcon } from "lucide-react"

import type { DeliverableStatus, Story } from "@/api/client"
import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { useDecisions, type LedgerEntry } from "./sitting-ledger"

/** Statuses in which the agent (or a render) is still busy with a draft. */
const WORKING: DeliverableStatus[] = [
  "queued",
  "drafting",
  "rendering",
  "finalizing",
  "publishing",
]

const outcome = {
  approved: { icon: CheckIcon, tone: "text-success" },
  sent: { icon: PencilLineIcon, tone: "text-primary-ink" },
  rejected: { icon: XIcon, tone: "text-destructive" },
} as const

/**
 * The end of a review sitting: what the reviewer decided on the drafts of
 * this sitting since the page loaded, and what is still under way. Counts
 * come from the decisions made here and the stories' latest known state.
 */
export function SittingSummary({
  stories,
  workspaceId,
}: {
  /** The sitting's stories, in feed order. */
  stories: Story[]
  workspaceId: string
}) {
  const all = useDecisions()
  const byId = new Map(stories.map((s) => [s.id, s]))
  const decisions = all.filter((e) => byId.has(e.storyId))
  const decided = (storyId: string, deliverableId: string, revision: number) =>
    decisions.some(
      (e) =>
        e.storyId === storyId &&
        e.deliverableId === deliverableId &&
        e.revision === revision
    )

  const count = (kind: LedgerEntry["kind"]) =>
    decisions.filter((e) => e.kind === kind).length
  const notes = decisions.reduce(
    (n, e) => (e.kind === "sent" ? n + e.notes : n),
    0
  )
  const waiting = stories.filter((s) =>
    s.deliverables.some(
      (d) => d.status === "review" && !decided(s.id, d.id, d.revision)
    )
  ).length
  const working = stories.filter((s) =>
    s.deliverables.some((d) => WORKING.includes(d.status))
  ).length

  return (
    <div className="grid w-full max-w-md gap-5 self-center">
      <div className="grid gap-1.5">
        <h2 className="text-lg font-semibold tracking-tight">
          {m.feed.endTitle}
        </h2>
        <p className="text-sm text-muted-foreground">
          {decisions.length > 0
            ? m.feed.summary(
                count("approved"),
                count("sent"),
                notes,
                count("rejected")
              )
            : m.feed.nothingDecided}
        </p>
      </div>
      {decisions.length > 0 ? (
        <ul
          className="divide-y border-y text-sm"
          aria-label={m.feed.decisions}
        >
          {decisions.map((e) => {
            const story = byId.get(e.storyId)
            const o = outcome[e.kind]
            const kind = story?.deliverables.find(
              (d) => d.id === e.deliverableId
            )?.kind
            return (
              <li
                key={`${e.deliverableId}/${e.revision}`}
                className="flex items-baseline gap-3 py-2.5"
              >
                <span
                  className={cn(
                    "flex w-24 shrink-0 items-center gap-1.5 self-center text-xs font-medium",
                    o.tone
                  )}
                >
                  <o.icon className="size-3.5" aria-hidden="true" />
                  {m.feed.outcome[e.kind]}
                </span>
                <span className="min-w-0 flex-1">
                  <span lang={story?.language} className="line-clamp-1">
                    {story?.title}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {kind ? (m.values.kind[kind] ?? kind) : null}
                    {kind ? " · " : null}
                    {m.stories.revisionShort(e.revision)}
                    {e.kind === "sent"
                      ? ` · ${m.feedbackSheet.pendingCount(e.notes)}`
                      : null}
                  </span>
                </span>
              </li>
            )
          })}
        </ul>
      ) : null}
      {waiting > 0 || working > 0 ? (
        <p className="text-sm text-muted-foreground">
          {[
            waiting > 0 ? m.feed.stillWaiting(waiting) : null,
            working > 0 ? m.feed.agentWorking(working) : null,
          ]
            .filter(Boolean)
            .join(" ")}
        </p>
      ) : (
        <p className="text-sm text-muted-foreground">{m.feed.endBody}</p>
      )}
      <div>
        <Button
          variant="outline"
          render={
            <Link to="/w/$workspaceId/stories" params={{ workspaceId }} />
          }
          nativeButton={false}
        >
          {m.feed.backToReview}
        </Button>
      </div>
    </div>
  )
}
