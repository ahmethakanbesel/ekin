import {
  CheckIcon,
  CircleCheckIcon,
  CircleXIcon,
  ClockIcon,
  FileTextIcon,
  Loader2Icon,
  VideoIcon,
  type LucideIcon,
} from "lucide-react"

import type { Deliverable, DeliverableStatus } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { deliverableBadge } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { isWorking } from "@/queries/stories"
import { deliverableLabel } from "./model"

/** Badge classes per deliverable status, from the shared tones. */
export function statusTone(status: DeliverableStatus): string {
  return deliverableBadge(status)
}

const statusIcon: Partial<Record<DeliverableStatus, LucideIcon>> = {
  queued: ClockIcon,
  ready: CheckIcon,
  published: CircleCheckIcon,
  failed: CircleXIcon,
}

/** A deliverable status; with `revision`, prefixed by it ("r3 · In review"). */
export function StatusBadge({
  status,
  revision,
  className,
}: {
  status: DeliverableStatus
  revision?: number
  className?: string
}) {
  const spinning = isWorking({ status }) && status !== "queued"
  const Icon = spinning ? Loader2Icon : statusIcon[status]
  return (
    <Badge variant="secondary" className={cn(statusTone(status), className)}>
      {Icon ? (
        <Icon
          data-icon="inline-start"
          className={cn(spinning && "animate-spin")}
        />
      ) : null}
      {revision ? `${m.stories.revisionShort(revision)} · ` : null}
      {m.stories.status[status] ?? status}
    </Badge>
  )
}

export function KindIcon({
  kind,
  className,
}: {
  kind: Deliverable["kind"]
  className?: string
}) {
  const Icon = kind === "video" ? VideoIcon : FileTextIcon
  return (
    <Icon
      aria-hidden
      className={cn("size-3.5 shrink-0 text-muted-foreground", className)}
    />
  )
}

/** One deliverable in a list row: kind icon, status and revision. */
export function DeliverableChip({
  deliverable: d,
}: {
  deliverable: Deliverable
}) {
  return (
    <span
      className="inline-flex items-center gap-1.5 text-sm"
      title={deliverableLabel(d)}
    >
      <KindIcon kind={d.kind} />
      {d.role === "translation" ? (
        <span className="font-mono text-[11px] text-muted-foreground uppercase">
          {d.language}
        </span>
      ) : null}
      <StatusBadge status={d.status} />
      {d.revision > 0 ? (
        <span className="tabular text-xs text-muted-foreground">
          {m.stories.revisionShort(d.revision)}
        </span>
      ) : null}
    </span>
  )
}
