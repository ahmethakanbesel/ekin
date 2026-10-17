import type { ReactNode } from "react"
import { AlertCircleIcon, Loader2Icon, LockIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { iconClass, toneClass } from "@/lib/tones"
import { cn } from "@/lib/utils"

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4">
      <div className="min-w-0 space-y-1">
        <h1 className="font-heading text-2xl font-semibold tracking-tight">
          {title}
        </h1>
        {description ? (
          <p className="text-sm text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex items-center gap-2">{actions}</div>
      ) : null}
    </div>
  )
}

export function EmptyState({
  icon,
  title,
  body,
  action,
  className,
}: {
  icon?: ReactNode
  title: string
  body?: string
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed px-6 py-14 text-center",
        className
      )}
    >
      {icon ? (
        <div className="flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground [&_svg]:size-6">
          {icon}
        </div>
      ) : null}
      <div className="max-w-md space-y-1">
        <h2 className="font-heading text-base font-medium">{title}</h2>
        {body ? <p className="text-sm text-muted-foreground">{body}</p> : null}
      </div>
      {action ? <div className="pt-2">{action}</div> : null}
    </div>
  )
}

export function ErrorState({
  error,
  onRetry,
}: {
  error: unknown
  onRetry?: () => void
}) {
  return (
    <div
      role="alert"
      className="flex flex-col items-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 px-6 py-10 text-center"
    >
      <AlertCircleIcon className="size-6 text-destructive" />
      <div className="space-y-1">
        <p className="font-medium">{m.errors.loadFailed}</p>
        <p className="text-sm text-muted-foreground">{errorMessage(error)}</p>
      </div>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          {m.common.retry}
        </Button>
      ) : null}
    </div>
  )
}

export function TableSkeleton({ rows = 4 }: { rows?: number }) {
  return (
    <div className="space-y-3" aria-busy="true" aria-label={m.common.loading}>
      <Skeleton className="h-9 w-full" />
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-12 w-full" />
      ))}
    </div>
  )
}

/** An inline error banner, used for server rejections on forms. */
export function Banner({
  title,
  children,
  action,
  tone = "destructive",
}: {
  title: string
  children?: ReactNode
  action?: ReactNode
  tone?: "destructive" | "warning"
}) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-wrap items-start justify-between gap-3 rounded-lg border px-4 py-3 text-sm",
        toneClass[tone === "destructive" ? "danger" : "attention"]
      )}
    >
      <div className="flex min-w-0 gap-2.5">
        <AlertCircleIcon
          className={cn(
            "mt-0.5 size-4 shrink-0",
            iconClass[tone === "destructive" ? "danger" : "attention"]
          )}
        />
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium">{title}</p>
          {children ? (
            <div className="text-muted-foreground">{children}</div>
          ) : null}
        </div>
      </div>
      {action}
    </div>
  )
}

/** Says why the controls on a page are missing or disabled for this role. */
export function ReadOnlyNote({
  children,
  className,
}: {
  children: ReactNode
  className?: string
}) {
  return (
    <p
      className={cn(
        "mb-4 flex items-center gap-2 text-sm text-muted-foreground",
        className
      )}
    >
      <LockIcon className="size-4 shrink-0" />
      {children}
    </p>
  )
}

/**
 * The foot of a paged list: loads the next page, says so while it does,
 * and is gone once the last page is in (or while a new search loads).
 */
export function LoadMore({
  query,
  size,
  className,
}: {
  query: {
    hasNextPage: boolean
    isFetchingNextPage: boolean
    /** Rows kept from the previous search while this one loads. */
    isPlaceholderData: boolean
    isFetchNextPageError: boolean
    error: unknown
    fetchNextPage: () => Promise<unknown>
  }
  size?: "sm" | "default"
  className?: string
}) {
  if (!query.hasNextPage || query.isPlaceholderData) return null
  const busy = query.isFetchingNextPage
  return (
    <div className={cn("flex flex-col items-center gap-2 pt-6", className)}>
      <Button
        variant="outline"
        size={size}
        disabled={busy}
        aria-busy={busy || undefined}
        onClick={() => void query.fetchNextPage()}
      >
        {busy ? (
          <Loader2Icon data-icon="inline-start" className="animate-spin" />
        ) : null}
        {busy ? m.common.loadingMore : m.common.loadMore}
      </Button>
      {query.isFetchNextPageError && !busy ? (
        <p role="alert" className="text-xs text-destructive">
          {errorMessage(query.error)}
        </p>
      ) : null}
    </div>
  )
}
