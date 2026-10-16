import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { queueFill } from "@/lib/tones"
import { cn } from "@/lib/utils"

/** A queue's hue as a small dot, beside its name. */
export function QueueDot({
  queue,
  className,
}: {
  queue: string
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn("size-2 shrink-0 rounded-full", queueFill(queue), className)}
    />
  )
}

/**
 * The queue a job runs on, by the name the throttle and filters use; the
 * id workers and logs print is its tooltip.
 */
export function QueueBadge({ queue }: { queue: string }) {
  return (
    <Badge variant="outline" className="rounded-md font-normal" title={queue}>
      <QueueDot queue={queue} className="size-1.5" />
      {m.system.queues[queue] ?? queue}
    </Badge>
  )
}
