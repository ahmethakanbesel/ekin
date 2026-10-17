import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ArrowUpRightIcon, CirclePauseIcon } from "lucide-react"

import type { HostProfile } from "@/api/client"
import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { iconClass, toneClass } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { meQuery } from "@/queries/auth"
import { profilesQuery } from "@/queries/system"

export type Throttle = {
  profiles: HostProfile[]
  /** The profile this server's worker uses. */
  current: string
}

/**
 * Host profiles for admins, to show which are paused. The throttle is a
 * server setting: it is changed on /system, never here.
 */
export function useThrottle(): Throttle | undefined {
  const me = useQuery(meQuery())
  const query = useQuery({
    ...profilesQuery(),
    enabled: !!me.data?.isAdmin,
    refetchInterval: 15_000,
  })
  if (!query.data) return undefined
  return { profiles: query.data.profiles, current: query.data.current }
}

/** One line per paused profile, with the way to where it is resumed. */
export function PausedBanners({ throttle }: { throttle: Throttle }) {
  const paused = throttle.profiles.filter((p) => p.paused)
  if (paused.length === 0) return null
  const t = m.jobs.throttle
  return (
    <div className="mb-4 grid gap-2">
      {paused.map((p) => (
        <div
          key={p.name}
          role="status"
          className={cn(
            "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border px-4 py-3 text-sm",
            toneClass.muted
          )}
        >
          <CirclePauseIcon className={cn("size-4 shrink-0", iconClass.muted)} />
          <span className="min-w-0 flex-1">
            {p.name === throttle.current ? t.pausedHere(p.name) : t.pausedRemote(p.name)}
          </span>
          <ServerLink />
        </div>
      ))}
    </div>
  )
}

/** Where the throttle is changed: Server › Host profiles. */
export function ServerLink({ className }: { className?: string }) {
  return (
    <Button
      size="sm"
      variant="outline"
      className={className}
      render={<Link to="/system" hash="profiles" />}
      nativeButton={false}
    >
      {m.jobs.throttle.change}
      <ArrowUpRightIcon data-icon="inline-end" />
    </Button>
  )
}
