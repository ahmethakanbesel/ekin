import type { ReactNode } from "react"
import { AlertCircleIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
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
        <h1 className="font-heading text-2xl font-semibold tracking-tight">{title}</h1>
        {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
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
        "flex flex-col items-center justify-center gap-3 rounded-4xl border border-dashed px-6 py-14 text-center",
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

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div
      role="alert"
      className="flex flex-col items-center gap-3 rounded-4xl border border-destructive/30 bg-destructive/5 px-6 py-10 text-center"
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
        "flex flex-wrap items-start justify-between gap-3 rounded-3xl border px-4 py-3 text-sm",
        tone === "destructive"
          ? "border-destructive/30 bg-destructive/5"
          : "border-amber-500/40 bg-amber-500/10"
      )}
    >
      <div className="flex min-w-0 gap-2.5">
        <AlertCircleIcon
          className={cn(
            "mt-0.5 size-4 shrink-0",
            tone === "destructive" ? "text-destructive" : "text-amber-600 dark:text-amber-400"
          )}
        />
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium">{title}</p>
          {children ? <div className="text-muted-foreground">{children}</div> : null}
        </div>
      </div>
      {action}
    </div>
  )
}
