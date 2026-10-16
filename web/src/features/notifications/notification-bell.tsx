import { useState, useSyncExternalStore } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  BellIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  InboxIcon,
  PlayIcon,
  type LucideIcon,
} from "lucide-react"

import type { Notification } from "@/api/client"
import {
  Popover,
  PopoverContent,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatAgo, formatDateTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { notificationsQuery } from "@/queries/notifications"

const kindIcon: Record<
  Notification["kind"],
  { icon: LucideIcon; tone: string }
> = {
  review: { icon: InboxIcon, tone: "text-primary-ink" },
  failed: { icon: CircleAlertIcon, tone: "text-destructive" },
  published: { icon: CircleCheckIcon, tone: "text-success" },
  run: { icon: PlayIcon, tone: "text-info" },
}

// When the list was last opened, per workspace, so the dot means "new
// since you looked". Storage can be unavailable (private mode, blocked
// site data); then every visit starts fresh.
// The sidebar and the mobile header each render a bell; they share the
// mark through this small store.
const storageKey = (workspaceId: string) =>
  `kurgu:notifications-seen:${workspaceId}`
const seenCache = new Map<string, number>()
const listeners = new Set<() => void>()

function readSeen(workspaceId: string): number {
  const cached = seenCache.get(workspaceId)
  if (cached !== undefined) return cached
  let t = 0
  try {
    const raw = window.localStorage.getItem(storageKey(workspaceId))
    t = raw ? Number(raw) || 0 : 0
  } catch {
    // unavailable: start fresh
  }
  seenCache.set(workspaceId, t)
  return t
}

function writeSeen(workspaceId: string, t: number) {
  seenCache.set(workspaceId, t)
  try {
    window.localStorage.setItem(storageKey(workspaceId), String(t))
  } catch {
    // remembered for this page only
  }
  for (const l of listeners) l()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function useSeen(workspaceId: string): number {
  return useSyncExternalStore(subscribe, () => readSeen(workspaceId))
}

const time = (iso: string) => new Date(iso).getTime() || 0

export function NotificationBell({
  workspaceId,
  timezone,
  className,
}: {
  workspaceId: string
  timezone: string
  className?: string
}) {
  const query = useQuery({
    ...notificationsQuery(workspaceId),
    refetchIntervalInBackground: true,
  })
  const seen = useSeen(workspaceId)
  // The mark the list was opened with, so items new on this opening stay
  // highlighted until it closes.
  const [openedWith, setOpenedWith] = useState<number | null>(null)
  const [open, setOpen] = useState(false)
  const now = useNow()

  const items = query.data ?? []
  // Server times, so a skewed browser clock cannot hide new items.
  const newest = items.reduce((max, n) => Math.max(max, time(n.createdAt)), 0)
  const unread = newest > seen

  const onOpenChange = (next: boolean) => {
    setOpen(next)
    if (next) {
      setOpenedWith(seen)
      if (newest > seen) writeSeen(workspaceId, newest)
    } else setOpenedWith(null)
  }

  let body
  if (query.isPending)
    body = (
      <div className="grid gap-2 p-2">
        <Skeleton className="h-12 w-full" />
        <Skeleton className="h-12 w-full" />
      </div>
    )
  else if (query.isError)
    body = (
      <p className="p-3 text-sm text-muted-foreground">
        {errorMessage(query.error)}
      </p>
    )
  else if (items.length === 0)
    body = (
      <p className="p-3 text-sm text-muted-foreground">
        {m.notifications.empty}
      </p>
    )
  else
    body = (
      <ul className="grid gap-0.5">
        {items.map((n) => (
          <li key={n.id}>
            <NotificationItem
              notification={n}
              workspaceId={workspaceId}
              timezone={timezone}
              fresh={openedWith !== null && time(n.createdAt) > openedWith}
              now={now}
              onNavigate={() => setOpen(false)}
            />
          </li>
        ))}
      </ul>
    )

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        className={cn(
          "relative flex size-9 shrink-0 items-center justify-center rounded-full text-sidebar-foreground/80 outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring aria-expanded:bg-sidebar-accent",
          className
        )}
        aria-label={unread ? m.notifications.openUnread : m.notifications.open}
        title={m.notifications.title}
      >
        <BellIcon className="size-4" />
        {unread ? (
          <span
            className="absolute top-1.5 right-1.5 size-2 rounded-full bg-primary-ink ring-2 ring-sidebar"
            aria-hidden="true"
          />
        ) : null}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[min(24rem,calc(100vw-2rem))] gap-2 p-2"
      >
        <PopoverTitle className="px-2 pt-1.5 font-heading text-sm font-semibold">
          {m.notifications.title}
        </PopoverTitle>
        <div className="max-h-[min(28rem,70vh)] overflow-y-auto">{body}</div>
      </PopoverContent>
    </Popover>
  )
}

function NotificationItem({
  notification: n,
  workspaceId,
  timezone,
  fresh,
  now,
  onNavigate,
}: {
  notification: Notification
  workspaceId: string
  timezone: string
  fresh: boolean
  now: number
  onNavigate: () => void
}) {
  const { icon: Icon, tone } = kindIcon[n.kind] ?? kindIcon.run
  const content = (
    <>
      <Icon
        className={cn("mt-0.5 size-4 shrink-0", tone)}
        aria-label={m.notifications.kinds[n.kind]}
      />
      <span className="grid min-w-0 flex-1 gap-0.5">
        <span className="flex items-start gap-2">
          <span className="min-w-0 flex-1 font-medium">{n.title}</span>
          {fresh ? (
            <span
              className="mt-1.5 size-2 shrink-0 rounded-full bg-primary-ink"
              aria-hidden="true"
            />
          ) : null}
        </span>
        {n.detail ? (
          <span className="line-clamp-2 text-xs text-muted-foreground">
            {n.detail}
          </span>
        ) : null}
        <span
          className="text-xs text-muted-foreground"
          title={formatDateTime(n.createdAt, timezone)}
        >
          {formatAgo(n.createdAt, now)}
        </span>
      </span>
    </>
  )
  const className = cn(
    "flex gap-3 rounded-lg px-2.5 py-2 text-sm",
    fresh && "bg-primary/5"
  )
  return n.storyId ? (
    <Link
      to="/w/$workspaceId/stories/$storyId"
      params={{ workspaceId, storyId: n.storyId }}
      onClick={onNavigate}
      className={cn(
        className,
        "outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring"
      )}
    >
      {content}
    </Link>
  ) : (
    <div className={className}>{content}</div>
  )
}
