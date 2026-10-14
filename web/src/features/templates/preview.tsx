import type { Template } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { KindIcon } from "@/features/stories/badges"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import "./fonts.css"
import { HEX, headingStyle, titleColours } from "./model"

type Look = Pick<Template, "name" | "palette" | "headingFont">

/**
 * A miniature title card: the name set in the heading font, on the first
 * palette colour, in the palette colour that reads best on it.
 */
export function TitleCard({
  template,
  size = "md",
  className,
}: {
  template: Look
  size?: "md" | "lg"
  className?: string
}) {
  const { background, text, accent } = titleColours(template.palette)
  return (
    <div
      className={cn(
        "relative flex aspect-video flex-col justify-end overflow-hidden",
        size === "lg" ? "gap-3 p-8" : "gap-2 p-5",
        className
      )}
      style={{ background, color: text }}
      aria-hidden="true"
    >
      {accent ? (
        <span
          className={cn("block rounded-full", size === "lg" ? "h-1.5 w-16" : "h-1 w-10")}
          style={{ background: accent }}
        />
      ) : null}
      <span
        className={cn(
          "line-clamp-2 leading-[1.1] tracking-tight break-words",
          size === "lg" ? "text-4xl sm:text-5xl" : "text-2xl"
        )}
        style={headingStyle(template.headingFont)}
      >
        {template.name || m.templates.form.namePlaceholder}
      </span>
    </div>
  )
}

/** A thumbnail of the title card, for select options. */
export function MiniCard({ template, className }: { template: Look; className?: string }) {
  const { background, text } = titleColours(template.palette)
  return (
    <span
      className={cn(
        "inline-flex h-6 w-9 shrink-0 items-center justify-center rounded-md text-[11px] leading-none ring-1 ring-foreground/10",
        className
      )}
      style={{ background, color: text, ...headingStyle(template.headingFont) }}
      aria-hidden="true"
    >
      Aa
    </span>
  )
}

/** The palette as one segmented strip. */
export function PaletteStrip({ palette, className }: { palette: string[]; className?: string }) {
  const colours = palette.filter((c) => HEX.test(c))
  if (colours.length === 0) {
    return <p className={cn("text-xs text-muted-foreground", className)}>{m.templates.noPalette}</p>
  }
  return (
    <div
      className={cn("flex h-3 overflow-hidden rounded-full ring-1 ring-foreground/10", className)}
      role="img"
      aria-label={`${m.templates.palette}: ${colours.join(", ")}`}
    >
      {colours.map((c, i) => (
        <span key={`${c}-${i}`} className="flex-1" style={{ background: c }} title={c} />
      ))}
    </div>
  )
}

/** The palette as labelled chips. */
export function PaletteChips({ palette }: { palette: string[] }) {
  const colours = palette.filter((c) => HEX.test(c))
  if (colours.length === 0) {
    return <p className="text-sm text-muted-foreground">{m.templates.noPalette}</p>
  }
  return (
    <ul className="flex flex-wrap gap-3">
      {colours.map((c, i) => (
        <li key={`${c}-${i}`} className="flex items-center gap-2">
          <span
            className="size-8 shrink-0 rounded-xl ring-1 ring-foreground/10"
            style={{ background: c }}
          />
          <span className="font-mono text-xs text-muted-foreground uppercase">{c}</span>
        </li>
      ))}
    </ul>
  )
}

const sourceVariant = {
  builtin: "outline",
  custom: "secondary",
  generated: "secondary",
} as const

export function SourceBadge({ source }: { source: Template["source"] }) {
  return (
    <Badge
      variant={sourceVariant[source] ?? "outline"}
      className={cn(
        source === "generated" && "bg-violet-500/15 text-violet-800 dark:text-violet-300"
      )}
    >
      {m.templates.source[source] ?? source}
    </Badge>
  )
}

export function KindBadges({ kinds }: { kinds: Template["kinds"] }) {
  return (
    <>
      {kinds.map((k) => (
        <Badge key={k} variant="outline" className="gap-1">
          <KindIcon kind={k} className="text-current" />
          {m.values.kind[k] ?? k}
        </Badge>
      ))}
    </>
  )
}
