import { CircleCheckIcon, CirclePauseIcon } from "lucide-react"

import type { Pipeline } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { badgeTone, pipelineHue } from "@/lib/tones"
import { cn } from "@/lib/utils"

/** The pipeline's identity hue, the same swatch its stories carry. */
export function PipelineSwatch({
  id,
  className,
}: {
  id: string
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn("size-2.5 shrink-0 rounded-[3px]", pipelineHue(id).bg, className)}
    />
  )
}

/**
 * Both states are neutral: green means a draft is done, and an active
 * pipeline is only running as expected. Active is a stone fill with a check,
 * paused an outline with a pause sign; the word carries the state.
 */
export function PipelineStatusBadge({ status }: { status: Pipeline["status"] }) {
  const active = status === "active"
  const Icon = active ? CircleCheckIcon : CirclePauseIcon
  return (
    <Badge
      variant={active ? "secondary" : "outline"}
      className={active ? undefined : badgeTone.neutral}
    >
      <Icon data-icon="inline-start" />
      {m.pipelines.status[status]}
    </Badge>
  )
}
