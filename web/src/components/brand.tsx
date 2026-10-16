import { Link } from "@tanstack/react-router"
import { ClapperboardIcon } from "lucide-react"

import { m } from "@/i18n"
import { cn } from "@/lib/utils"

/** The marigold logo square. */
export function BrandMark({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground inset-ring inset-ring-primary-ink",
        className
      )}
      aria-hidden="true"
    >
      <ClapperboardIcon className="size-4" />
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
