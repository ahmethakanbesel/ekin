import { Link } from "@tanstack/react-router"

import { m } from "@/i18n"

export function Brand() {
  return (
    <Link to="/" className="flex items-center gap-2 px-2.5 py-1 outline-none">
      <span className="font-heading text-lg font-semibold tracking-tight">{m.app.name}</span>
      <span className="hidden text-xs text-muted-foreground lg:inline">{m.app.tagline}</span>
    </Link>
  )
}
