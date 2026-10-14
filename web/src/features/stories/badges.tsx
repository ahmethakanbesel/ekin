import { FileTextIcon, Loader2Icon, VideoIcon } from "lucide-react"

import type { Deliverable, DeliverableStatus } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { isWorking } from "@/queries/stories"
import { deliverableLabel } from "./model"

export const statusTone: Record<DeliverableStatus, string> = {
  waiting: "bg-muted text-muted-foreground",
  queued: "bg-secondary text-secondary-foreground",
  drafting: "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  rendering: "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  review: "bg-amber-500/15 text-amber-800 dark:text-amber-300",
  finalizing: "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  ready: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  publishing: "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  published: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  rejected: "bg-muted text-muted-foreground line-through decoration-1",
  failed: "bg-destructive/10 text-destructive",
}

export function StatusBadge({ status, className }: { status: DeliverableStatus; className?: string }) {
  const spinning = isWorking({ status }) && status !== "queued"
  return (
    <Badge variant="secondary" className={cn(statusTone[status], className)}>
      {spinning ? <Loader2Icon data-icon="inline-start" className="animate-spin" /> : null}
      {m.stories.status[status] ?? status}
    </Badge>
  )
}

export function KindIcon({ kind, className }: { kind: Deliverable["kind"]; className?: string }) {
  const Icon = kind === "video" ? VideoIcon : FileTextIcon
  return <Icon aria-hidden className={cn("size-3.5 shrink-0 text-muted-foreground", className)} />
}

/** One deliverable in a list row: kind icon, status and revision. */
export function DeliverableChip({ deliverable: d }: { deliverable: Deliverable }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-sm" title={deliverableLabel(d)}>
      <KindIcon kind={d.kind} />
      {d.role === "translation" ? (
        <span className="font-mono text-[11px] text-muted-foreground uppercase">{d.language}</span>
      ) : null}
      <StatusBadge status={d.status} />
      {d.revision > 0 ? (
        <span className="text-xs text-muted-foreground tabular">
          {m.stories.revisionShort(d.revision)}
        </span>
      ) : null}
    </span>
  )
}
