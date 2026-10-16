import type { ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  CheckIcon,
  MessageSquareIcon,
  PencilIcon,
  SparklesIcon,
  type LucideIcon,
} from "lucide-react"

import type { Deliverable, Revision } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { membersQuery } from "@/queries/members"
import { FeedbackLine } from "./status-alert"

/** Names for revisions a person edited in place (any member may list members). */
function useEditorNames(workspaceId: string, revisions: Revision[]) {
  const members = useQuery({
    ...membersQuery(workspaceId),
    enabled: revisions.some((r) => r.editedBy),
  })
  return (userId: string) =>
    members.data?.find((x) => x.userId.toLowerCase() === userId.toLowerCase())
      ?.name
}

/** How many entries the history lists. */
export function historyCount(d: Deliverable, revisions: Revision[]): number {
  return revisions.length + (pendingNote(d) ? 1 : 0)
}

const pendingNote = (d: Deliverable) =>
  d.pendingFeedback && d.status !== "review" ? d.pendingFeedback : undefined

/**
 * What happened to the deliverable, newest first: each revision with the
 * note that asked for it and the agent's notes on what it changed (or who
 * edited it), and a note still waiting for its revision.
 */
export function RevisionHistory({
  workspaceId,
  deliverable: d,
  revisions,
  loading,
  shown,
  timezone,
}: {
  workspaceId: string
  deliverable: Deliverable
  revisions: Revision[]
  loading: boolean
  /** The revision on screen. */
  shown: number | undefined
  timezone: string
}) {
  const now = useNow()
  const editorName = useEditorNames(workspaceId, revisions)
  const pending = pendingNote(d)

  if (loading) {
    return (
      <div className="grid gap-3" aria-busy="true" aria-label={m.common.loading}>
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    )
  }
  if (revisions.length === 0 && !pending) {
    return <p className="text-sm text-muted-foreground">{m.revisions.empty}</p>
  }

  return (
    <ol className="grid gap-5">
      {pending ? (
        <Entry icon={MessageSquareIcon} tone="text-info">
          <p className="text-sm font-medium text-info">
            {m.revisions.pending(d.revision + 1)}
          </p>
          <div className="mt-1 text-sm">
            <FeedbackLine feedback={pending} kind={d.kind} />
          </div>
        </Entry>
      ) : null}
      {revisions.toReversed().map((r) => {
        const editor = r.editedBy ? editorName(r.editedBy) : undefined
        return (
          <Entry
            key={r.number}
            icon={r.editedBy ? PencilIcon : SparklesIcon}
            current={r.number === shown}
          >
            <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
              <Badge variant="outline" className="tabular">
                {m.stories.revisionShort(r.number)}
              </Badge>
              <span className="font-medium">
                {r.editedBy
                  ? editor
                    ? m.revisions.editedBy(editor)
                    : m.revisions.editedInPlace
                  : m.history.written}
              </span>
              <span
                className="ml-auto text-xs text-muted-foreground"
                title={formatDateTime(r.createdAt, timezone)}
              >
                {formatRelative(r.createdAt, now)}
              </span>
            </p>
            {r.feedback ? (
              <div className="mt-2 grid gap-1.5 rounded-lg bg-muted/60 px-3 py-2.5 text-sm">
                <span className="text-xs font-medium text-muted-foreground">
                  {m.history.askedOn(r.number - 1)}
                </span>
                <FeedbackLine feedback={r.feedback} kind={d.kind} />
                <span className="flex items-center gap-1.5 text-xs text-success">
                  <CheckIcon className="size-3.5" />
                  {m.history.addressedIn(r.number)}
                </span>
              </div>
            ) : null}
            {r.editedBy ? null : (
              <p
                className="mt-2 text-sm whitespace-pre-wrap text-muted-foreground"
                title={r.notes.trim() || undefined}
              >
                <span className="sr-only">{m.revisions.notes}: </span>
                {r.notes.trim() || m.revisions.noNotes}
              </p>
            )}
          </Entry>
        )
      })}
    </ol>
  )
}

function Entry({
  icon: Icon,
  tone,
  current,
  children,
}: {
  icon: LucideIcon
  tone?: string
  current?: boolean
  children: ReactNode
}) {
  return (
    <li className="flex gap-3" aria-current={current ? "true" : undefined}>
      <span
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground",
          current && "text-foreground inset-ring inset-ring-input",
          tone
        )}
        aria-hidden="true"
      >
        <Icon className="size-4" />
      </span>
      <div className="min-w-0 flex-1">{children}</div>
    </li>
  )
}
