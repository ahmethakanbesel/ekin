import { useQuery } from "@tanstack/react-query"

import type { PageSearch, SearchTotals, Story } from "@/api/client"
import { m } from "@/i18n"
import { formatInteger } from "@/lib/format"
import { cn } from "@/lib/utils"
import { storySearchQuery } from "@/queries/search"

const t = m.search

/** Whether a story has a page live on the site to measure. */
export function hasLivePage(story: Story): boolean {
  return story.deliverables.some((d) => d.status === "published" && d.kind !== "video")
}

/**
 * How the story's live pages did in search: per page, its 28 days before
 * against the days since, per day so the windows compare.
 */
export function StorySearch({ workspaceId, story }: { workspaceId: string; story: Story }) {
  const query = useQuery(storySearchQuery(workspaceId, story.id))
  if (query.isPending || query.isError) return null
  if (query.data.pages.length === 0) return <p className="text-sm text-muted-foreground">{t.noPages}</p>
  return (
    <ul className="grid gap-4">
      {query.data.pages.map((p) => (
        <PageRow key={p.url} page={p} />
      ))}
    </ul>
  )
}

function PageRow({ page: p }: { page: PageSearch }) {
  // Live since the last day synced: nothing to compare with yet.
  const fresh = p.after.days === 0
  const rows: { label: string; before: string; after: string; better?: boolean }[] = [
    {
      label: t.clicks,
      before: t.perDay(perDay(p.before.clicks, p.before)),
      after: t.perDay(perDay(p.after.clicks, p.after)),
      better: compare(perDay(p.after.clicks, p.after), perDay(p.before.clicks, p.before)),
    },
    {
      label: t.impressions,
      before: t.perDay(perDay(p.before.impressions, p.before)),
      after: t.perDay(perDay(p.after.impressions, p.after)),
      better: compare(perDay(p.after.impressions, p.after), perDay(p.before.impressions, p.before)),
    },
    {
      label: t.position,
      before: p.before.position ? p.before.position.toFixed(1) : "—",
      after: p.after.position ? p.after.position.toFixed(1) : "—",
      // Lower is better.
      better: p.before.position && p.after.position ? compare(p.before.position, p.after.position) : undefined,
    },
  ]
  return (
    <li className="grid gap-2">
      <a href={p.url} target="_blank" rel="noopener noreferrer" className="truncate font-mono text-xs hover:underline" title={p.url}>
        {p.url}
      </a>
      <p className="text-xs text-muted-foreground">{fresh ? t.storyFresh : t.storyBody(p.after.days)}</p>
      <table className="w-full text-sm">
        <thead>
          <tr className="text-xs text-muted-foreground">
            <th className="text-left font-normal">
              <span className="sr-only">{t.metric}</span>
            </th>
            <th className="text-right font-normal">{t.before}</th>
            <th className="text-right font-normal">{t.since}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.label}>
              <th className="py-0.5 text-left font-normal text-muted-foreground">{r.label}</th>
              <td className="tabular text-right">{r.before}</td>
              <td
                className={cn(
                  "tabular text-right font-medium",
                  !fresh && r.better === true && "text-success",
                  !fresh && r.better === false && "text-destructive"
                )}
              >
                {fresh ? "—" : r.after}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {fresh ? null : (
        <p className="tabular text-xs text-muted-foreground">
          {formatInteger(p.before.clicks)} → {formatInteger(p.after.clicks)} {t.clicks.toLowerCase()}
        </p>
      )}
    </li>
  )
}

function perDay(n: number, totals: SearchTotals): number {
  return totals.days ? n / totals.days : 0
}

/** True when a beats b clearly, false when it is clearly worse, undefined when about even. */
function compare(a: number, b: number): boolean | undefined {
  if (!b && !a) return undefined
  if (!b) return true
  const r = (a - b) / b
  return Math.abs(r) < 0.05 ? undefined : r > 0
}
