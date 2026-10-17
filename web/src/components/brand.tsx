import { Link } from "@tanstack/react-router"

import { m } from "@/i18n"
import { cn } from "@/lib/utils"

/** The sprout: a stem and two leaves (brand/mark.svg). */
export function SproutMark({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      <path d="M12 21V11" />
      <path d="M12 14c0-3.3-2.7-6-6-6 0 3.3 2.7 6 6 6Z" fill="currentColor" />
      <path d="M12 11c0-4.4 3.1-7 7-7 0 4.4-3.1 7-7 7Z" fill="currentColor" />
    </svg>
  )
}

/** The blue logo square. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground inset-ring inset-ring-primary-ink",
        className
      )}
      aria-hidden="true"
    >
      <SproutMark className="size-4" />
    </span>
  )
}

/** Logo and name, linking to the workspace list. */
export function Brand({ tagline = true }: { tagline?: boolean }) {
  return (
    <Link
      to="/"
      className="flex items-center gap-2 rounded-lg outline-none focus-visible:ring-[3px] focus-visible:ring-ring"
    >
      <BrandMark />
      <span className="grid leading-tight">
        <span className="font-heading text-sm font-semibold tracking-tight">{m.app.name}</span>
        {tagline ? (
          <span className="hidden text-xs text-muted-foreground lg:inline">{m.app.tagline}</span>
        ) : null}
      </span>
    </Link>
  )
}
