import { useQuery } from "@tanstack/react-query"
import { MessageSquareIcon, PencilIcon } from "lucide-react"

import type { Deliverable, Revision } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { membersQuery } from "@/queries/members"
import { FeedbackLine } from "./status-alert"

/** Names for revisions a person edited in place (any member may list members). */
export function useEditorNames(workspaceId: string, revisions: Revision[]) {
  const members = useQuery({
    ...membersQuery(workspaceId),
    enabled: revisions.some((r) => r.editedBy),
  })
  return (userId: string) =>
    members.data?.find((x) => x.userId.toLowerCase() === userId.toLowerCase())
      ?.name
}

/** Every revision, newest first, with the feedback that led to it and the agent's notes. */
export function RevisionList({
  workspaceId,
  deliverable: d,
  revisions,
  loading,
  current,
  latest,
  timezone,
  onPick,
}: {
  workspaceId: string
  deliverable: Deliverable
  revisions: Revision[]
  loading: boolean
  current: number | undefined
  latest: number | undefined
  timezone: string
  onPick: (n: number) => void
}) {
  const now = useNow()
  const newestFirst = revisions.toReversed()
  const editorName = useEditorNames(workspaceId, revisions)
  return (
    <aside
      className="grid gap-3 lg:sticky lg:top-4"
      aria-label={m.revisions.title}
    >
      <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {m.revisions.title}
      </h3>
      {d.pendingFeedback && d.status !== "review" ? (
        <div className="grid gap-1.5 rounded-lg border border-dashed border-info/40 p-3.5 text-sm">
          <p className="flex items-center gap-1.5 text-xs font-medium text-info">
            <MessageSquareIcon className="size-3.5" />
            {m.revisions.pending(d.revision + 1)}
          </p>
          <FeedbackLine feedback={d.pendingFeedback} kind={d.kind} />
        </div>
      ) : null}
      {loading ? (
        <div className="grid gap-2">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : newestFirst.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
          {m.revisions.empty}
        </p>
      ) : (
        <ol className="grid gap-2">
          {newestFirst.map((r) => {
            const selected = r.number === current
            const editor = r.editedBy ? editorName(r.editedBy) : undefined
            return (
              <li key={r.number}>
                <button
                  type="button"
                  aria-pressed={selected}
                  onClick={() => onPick(r.number)}
                  className={cn(
                    "grid w-full gap-2 rounded-xl border p-3.5 text-left text-sm transition-colors outline-none hover:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-ring",
                    selected &&
                      "border-primary-ink/60 bg-primary/[0.06] hover:bg-primary/[0.06]"
                  )}
                >
                  <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <span className="font-medium">
                      {m.revisions.revision(r.number)}
                    </span>
                    {r.number === latest ? (
                      <Badge
                        variant="secondary"
                        className="h-4 px-1.5 text-[10px]"
                      >
                        {m.revisions.latest}
                      </Badge>
                    ) : null}
                    <span
                      className="ml-auto text-xs text-muted-foreground"
                      title={formatDateTime(r.createdAt, timezone)}
                    >
                      {formatRelative(r.createdAt, now)}
                    </span>
                  </span>
                  {r.feedback ? (
                    <span className="grid gap-1 rounded-lg bg-muted/60 px-3 py-2 text-xs">
                      <span className="font-medium text-muted-foreground">
                        {m.revisions.feedback}
                      </span>
                      <FeedbackLine feedback={r.feedback} kind={d.kind} />
                    </span>
                  ) : null}
                  {r.editedBy ? (
                    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                      <PencilIcon className="size-3.5" />
                      {editor
                        ? m.revisions.editedBy(editor)
                        : m.revisions.editedInPlace}
                    </span>
                  ) : (
                    <span className="grid gap-0.5 text-xs">
                      <span className="font-medium text-muted-foreground">
                        {m.revisions.notes}
                      </span>
                      <span
                        className={cn(
                          "whitespace-pre-wrap text-foreground/85",
                          !selected && "line-clamp-3"
                        )}
                      >
                        {r.notes.trim() || m.revisions.noNotes}
                      </span>
                    </span>
                  )}
                </button>
              </li>
            )
          })}
        </ol>
      )}
    </aside>
  )
}
