import { useId, type ReactNode } from "react"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"

// Settings that inherit: each row shows what the layer above gives beside
// what this layer sets, so where a value comes from is on the row itself.
// Server → workspace → pipeline → request; an unset value inherits.

/** What a row inherits: the value and the layer it comes from ("Server", "acme"). */
export type Inherited = { value: ReactNode; source: string }

/** The select value standing for "inherit" (a select item needs a value). */
export const INHERIT = "__inherit"

/** Desktop grid of a row: setting · inherited · this layer · reset. */
const GRID = "md:grid md:grid-cols-[minmax(8rem,11rem)_minmax(0,1fr)_minmax(13rem,20rem)_4.5rem] md:items-center md:gap-x-5"

/** Placeholder rows in the layered grid's own columns, so nothing jumps on load. */
export function LayeredSkeleton({ rows = 8 }: { rows?: number }) {
  return (
    <div aria-busy="true" aria-label={m.common.loading} className="pb-4">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className={cn("grid gap-2 px-6 py-3", GRID)}>
          <Skeleton className="h-4 w-28" />
          <Skeleton className="h-4 w-36" />
          <Skeleton className="h-9 w-full" />
          <span />
        </div>
      ))}
    </div>
  )
}

/** A small outline pill naming a layer. */
export function SourceChip({ children, here = false }: { children: ReactNode; here?: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex h-5 shrink-0 items-center rounded-full border px-2 text-xs font-medium whitespace-nowrap",
        here
          ? "border-transparent bg-secondary text-foreground inset-ring inset-ring-foreground/70"
          : "text-muted-foreground"
      )}
    >
      {children}
    </span>
  )
}

/**
 * A card of layered rows with a column head: "From Server" beside
 * "acme". `summary` sits by the title; `footer` holds the save bar.
 */
export function LayeredCard({
  id,
  title,
  description,
  summary,
  fromLabel,
  hereLabel,
  footer,
  className,
  children,
}: {
  id?: string
  className?: string
  /** Omitted where the section heading already names the card. */
  title?: string
  description?: ReactNode
  summary?: ReactNode
  fromLabel: string
  hereLabel: string
  footer?: ReactNode
  children: ReactNode
}) {
  return (
    <Card id={id} className={cn("gap-0 pb-0", className)}>
      <CardHeader className="pb-4">
        <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
          {title ? <CardTitle>{title}</CardTitle> : null}
          {description && !title ? (
            <CardDescription className="max-w-prose">{description}</CardDescription>
          ) : null}
          {summary ? <p className="text-sm text-muted-foreground">{summary}</p> : null}
        </div>
        {description && title ? <CardDescription>{description}</CardDescription> : null}
      </CardHeader>
      <div
        aria-hidden
        className={cn(
          "hidden border-b px-6 pb-2 text-xs font-medium text-muted-foreground",
          GRID
        )}
      >
        <span>{m.settings.columns.setting}</span>
        <span>{fromLabel}</span>
        <span>{hereLabel}</span>
        <span />
      </div>
      <CardContent className="px-0">{children}</CardContent>
      {footer}
    </Card>
  )
}

/** A titled run of rows inside a LayeredCard. */
export function LayeredGroup({ title, children }: { title: string; children: ReactNode }) {
  const id = useId()
  return (
    <section aria-labelledby={id} className="pt-4 first:pt-3">
      <h3 id={id} className="px-6 pb-1 text-xs font-semibold">
        {title}
      </h3>
      <div className="divide-y">{children}</div>
    </section>
  )
}

/**
 * One setting: its label, what it inherits, this layer's control and,
 * while set here, Reset. Below `md` the inherited value folds under the
 * control. `children` gets the id the control should be described by.
 */
export function LayeredRow({
  label,
  htmlFor,
  hint,
  inherited,
  set,
  dirty = false,
  onReset,
  readOnly = false,
  children,
}: {
  label: string
  htmlFor?: string
  hint?: ReactNode
  inherited: Inherited
  /** Whether this layer sets its own value. */
  set: boolean
  /** Whether the row differs from what is saved. */
  dirty?: boolean
  onReset: () => void
  readOnly?: boolean
  children: (describedBy: string) => ReactNode
}) {
  const id = useId()
  const inheritedId = `${id}-inherited`
  const hintId = `${id}-hint`
  const describedBy = hint ? `${inheritedId} ${hintId}` : inheritedId
  return (
    <div className={cn("grid gap-2 px-6 py-3", GRID)}>
      <div className="min-w-0">
        <label htmlFor={htmlFor} className="text-sm font-medium">
          {label}
        </label>
        {dirty ? (
          <span className="ml-2 inline-flex items-center gap-1 text-xs text-muted-foreground md:ml-0 md:flex">
            <span aria-hidden className="size-1.5 rounded-full bg-foreground/70" />
            {m.settings.unsavedRow}
          </span>
        ) : null}
      </div>
      <p
        id={inheritedId}
        className="order-last flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground md:order-none"
      >
        <span className="sr-only">{m.settings.inheritedFrom(inherited.source)}</span>
        <span
          className={cn(
            "min-w-0 truncate",
            set && "line-through decoration-muted-foreground/60"
          )}
        >
          {inherited.value}
        </span>
        <SourceChip>{inherited.source}</SourceChip>
        {set ? <span className="sr-only">{m.settings.overridden}</span> : null}
      </p>
      <div className="min-w-0">{children(describedBy)}</div>
      <div className="order-last -mt-1 md:order-none md:mt-0 md:justify-self-end">
        {set && !readOnly ? (
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto px-0 text-foreground underline decoration-input underline-offset-4 hover:decoration-foreground"
            aria-label={m.settings.resetLabel(label, inherited.source)}
            onClick={onReset}
          >
            {m.settings.reset}
          </Button>
        ) : null}
      </div>
      {hint ? (
        <p
          id={hintId}
          className="order-last max-w-prose text-xs text-muted-foreground md:order-none md:col-span-2 md:col-start-2 md:-mt-1 md:pt-0"
        >
          {hint}
        </p>
      ) : null}
    </div>
  )
}

/**
 * A select whose first option inherits ("Use the server's · 1080p"); a
 * value set here carries a "Set here" chip in the trigger.
 */
export function LayeredSelect({
  id,
  value,
  options,
  inheritLabel,
  describedBy,
  disabled,
  onChange,
}: {
  id: string
  /** "" inherits. */
  value: string
  options: { value: string; label: string }[]
  inheritLabel: string
  describedBy?: string
  disabled?: boolean
  onChange: (value: string) => void
}) {
  const items = [{ value: INHERIT, label: inheritLabel }, ...options]
  // A value set through the API that the list does not offer still shows.
  if (value && !options.some((o) => o.value === value)) items.push({ value, label: value })
  const set = value !== ""
  return (
    <Select
      items={items}
      disabled={disabled}
      value={set ? value : INHERIT}
      onValueChange={(v) => v != null && onChange(v === INHERIT ? "" : String(v))}
    >
      <SelectTrigger
        id={id}
        aria-describedby={describedBy}
        className={cn("w-full", set ? "font-medium" : "text-muted-foreground")}
      >
        <SelectValue />
        {set ? <SourceChip here>{m.settings.setHere}</SourceChip> : null}
      </SelectTrigger>
      <SelectContent>
        {items.map((it) => (
          <SelectItem key={it.value} value={it.value}>
            {it.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** "2 of 9 set in acme". */
export function setCount(values: unknown[]): number {
  return values.filter((v) => v !== "" && v !== 0 && v !== undefined && v !== null).length
}
