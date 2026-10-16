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
import { badgeTone, deliverableBadge } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { isWorking } from "@/queries/stories"

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

/**
 * A deliverable status; with `revision`, prefixed by it ("r3 · In review").
 * `quiet` drops the cornflower from "In review" where something nearby
 * (a tab, the deliverable switcher, the panel's banner) already says it.
 */
export function StatusBadge({
  status,
  revision,
  quiet = false,
  className,
}: {
  status: DeliverableStatus
  revision?: number
  quiet?: boolean
  className?: string
}) {
  const spinning = isWorking({ status }) && status !== "queued"
  const Icon = spinning ? Loader2Icon : statusIcon[status]
  return (
    <Badge
      variant="secondary"
      className={cn(
        quiet && status === "review" ? badgeTone.neutral : statusTone(status),
        className
      )}
    >
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
