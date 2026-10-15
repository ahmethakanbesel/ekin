import { useMemo } from "react"
import { useQuery } from "@tanstack/react-query"

import type { Document, DocumentSection } from "@/api/client"
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxTrigger,
  ComboboxValue,
} from "@/components/ui/combobox"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { documentContentsQuery } from "@/queries/documents"

/** The table of contents of a ready document; empty while loading, failed or absent. */
export function useContents(workspaceId: string, doc: Document | undefined): DocumentSection[] {
  const query = useQuery({
    ...documentContentsQuery(workspaceId, doc ?? { id: "", updatedAt: "" }),
    enabled: doc?.status === "ready",
  })
  return query.data ?? []
}

/** The sections that contain page, outermost first: the chapter, then its sections. */
export function sectionsAt(sections: DocumentSection[], page: number): DocumentSection[] {
  const trail: DocumentSection[] = []
  for (const s of sections) {
    if (page < s.fromPage || page > s.toPage) continue
    while (trail.length > 0 && trail[trail.length - 1].level >= s.level) trail.pop()
    trail.push(s)
  }
  return trail
}

/** Each section's parent (its index, or -1 at the top level) and whether it has children. */
export function sectionTree(sections: DocumentSection[]): {
  parent: number[]
  hasChildren: boolean[]
} {
  const parent: number[] = []
  const hasChildren = sections.map(() => false)
  const open: number[] = [] // the sections enclosing the current one
  sections.forEach((s, i) => {
    while (open.length > 0 && sections[open[open.length - 1]].level >= s.level) open.pop()
    const p = open.length > 0 ? open[open.length - 1] : -1
    parent.push(p)
    if (p >= 0) hasChildren[p] = true
    open.push(i)
  })
  return { parent, hasChildren }
}

/** The outermost section spanning exactly from–to, if any. */
export function sectionFor(
  sections: DocumentSection[],
  from: number | undefined,
  to: number | undefined
): DocumentSection | undefined {
  return sections.find((s) => s.fromPage === from && s.toPage === to)
}

/** Left padding for a section's level; deeper than 4 levels stays at 4. */
export function indent(level: number): string {
  return `${0.75 + Math.min(level - 1, 3) * 0.875}rem`
}

/**
 * Picks a chapter or section of a document to fill a reference's page
 * range. Renders nothing for a document without a table of contents.
 */
export function SectionPicker({
  id,
  workspaceId,
  doc,
  from,
  to,
  disabled,
  onPick,
  className,
}: {
  id?: string
  workspaceId: string
  doc: Document | undefined
  from: number | undefined
  to: number | undefined
  disabled?: boolean
  onPick: (section: DocumentSection) => void
  className?: string
}) {
  const sections = useContents(workspaceId, doc)
  const items = useMemo(() => sections.map((_, i) => i), [sections])
  if (sections.length === 0) return null
  const current = sectionFor(sections, from, to)
  const t = m.knowledge.contents

  return (
    <Combobox
      items={items}
      value={current ? sections.indexOf(current) : null}
      itemToStringLabel={(i: number) => sections[i]?.title ?? ""}
      // Items are indexes, so match on the title (case-insensitively).
      filter={(i: number, query: string) =>
        (sections[i]?.title ?? "").toLocaleLowerCase().includes(query.trim().toLocaleLowerCase())
      }
      onValueChange={(i: number | null) => {
        if (i !== null && sections[i]) onPick(sections[i])
      }}
      disabled={disabled}
    >
      <ComboboxTrigger
        id={id}
        disabled={disabled}
        className={cn(
          "flex h-8 w-full min-w-0 items-center justify-between gap-1.5 rounded-3xl border border-dashed px-3 text-sm whitespace-nowrap transition-[color,box-shadow,background-color] outline-none",
          "hover:bg-muted focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/30",
          "disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
      >
        <span className={cn("truncate", !current && "text-muted-foreground")}>
          <ComboboxValue placeholder={t.pick(sections.length)} />
        </span>
      </ComboboxTrigger>
      <ComboboxContent className="min-w-96">
        <ComboboxInput placeholder={t.search} showTrigger={false} />
        <ComboboxEmpty>{t.noMatch}</ComboboxEmpty>
        <ComboboxList>
          {(i: number) => {
            const s = sections[i]
            return (
              <ComboboxItem key={i} value={i} style={{ paddingLeft: indent(s.level) }}>
                <span className={cn("min-w-0 flex-1 truncate", s.level > 1 && "font-normal")}>
                  {s.title}
                </span>
                <span className="tabular ml-3 shrink-0 text-xs text-muted-foreground">
                  {m.knowledge.viewer.rangeSummary(s.fromPage, s.toPage)}
                </span>
              </ComboboxItem>
            )
          }}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  )
}
