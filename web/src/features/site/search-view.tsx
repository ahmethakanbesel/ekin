import { useState } from "react"
import { Link } from "@tanstack/react-router"
import { useQuery } from "@tanstack/react-query"
import { SearchIcon } from "lucide-react"

import type { SearchKey, SearchTotals } from "@/api/client"
import { EmptyState, ErrorState, TableSkeleton } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatInteger } from "@/lib/format"
import { cn } from "@/lib/utils"
import { searchConsoleQuery, searchOverviewQuery } from "@/queries/search"
import { shortURL } from "./rules"

const t = m.search
const WINDOWS = [7, 28, 90] as const

/** The site's search figures from Search Console: totals against the period before, top pages and queries. */
export function SearchView({ root }: { root: string }) {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const [days, setDays] = useState<number>(28)
  const status = useQuery(searchConsoleQuery(workspace.id))
  const overview = useQuery({ ...searchOverviewQuery(workspace.id, days), enabled: !!status.data?.syncedThrough })

  if (status.isPending) return <TableSkeleton rows={4} />
  if (status.isError) return <ErrorState error={status.error} onRetry={() => void status.refetch()} />
  if (!status.data.connected || !status.data.syncedThrough)
    return (
      <EmptyState
        icon={<SearchIcon />}
        title={status.data.connected ? t.waitingTitle : t.notConnectedTitle}
        body={status.data.connected ? t.waitingBody : t.notConnectedBody}
        action={
          canManage && !status.data.connected ? (
            <Button
              variant="outline"
              render={<Link to="/w/$workspaceId/settings/publishing" params={{ workspaceId: workspace.id }} />}
              nativeButton={false}
            >
              {t.connect}
            </Button>
          ) : null
        }
      />
    )

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">
          {overview.data?.start && overview.data.end ? t.range(overview.data.start, overview.data.end) : null}
        </p>
        <Tabs value={String(days)} onValueChange={(v) => setDays(Number(v))}>
          <TabsList aria-label={t.window}>
            {WINDOWS.map((d) => (
              <TabsTrigger key={d} value={String(d)}>
                {t.days(d)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
      {overview.isPending ? (
        <TableSkeleton rows={4} />
      ) : overview.isError ? (
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      ) : overview.data.current && overview.data.previous ? (
        <>
          <Stats current={overview.data.current} previous={overview.data.previous} />
          <div className="grid gap-6 xl:grid-cols-2">
            <KeyTable title={t.topPages} rows={overview.data.pages} render={(k) => shortURL(k, root)} link />
            <KeyTable title={t.topQueries} rows={overview.data.queries} render={(k) => k} />
          </div>
        </>
      ) : null}
    </div>
  )
}

const ctr = (x: SearchTotals) => (x.impressions ? x.clicks / x.impressions : 0)

function Stats({ current, previous }: { current: SearchTotals; previous: SearchTotals }) {
  const items = [
    { label: t.clicks, value: formatInteger(current.clicks), delta: change(current.clicks, previous.clicks), up: true },
    { label: t.impressions, value: formatInteger(current.impressions), delta: change(current.impressions, previous.impressions), up: true },
    { label: t.ctr, value: percent(ctr(current)), delta: change(ctr(current), ctr(previous)), up: true },
    // A lower position is better.
    { label: t.position, value: current.position ? current.position.toFixed(1) : "—", delta: change(current.position, previous.position), up: false },
  ]
  return (
    <dl className="grid grid-cols-2 gap-3 md:grid-cols-4">
      {items.map((it) => (
        <div key={it.label} className="rounded-xl border px-4 py-3">
          <dt className="text-xs text-muted-foreground">{it.label}</dt>
          <dd className="mt-1 flex items-baseline gap-2">
            <span className="tabular text-2xl font-semibold">{it.value}</span>
            {it.delta !== undefined ? (
              <span
                className={cn(
                  "tabular text-xs",
                  it.delta === 0 ? "text-muted-foreground" : (it.delta > 0) === it.up ? "text-success" : "text-destructive"
                )}
                title={t.versusBefore}
              >
                {it.delta > 0 ? "+" : ""}
                {Math.round(it.delta * 100)}%
              </span>
            ) : null}
          </dd>
        </div>
      ))}
    </dl>
  )
}

/** Relative change; undefined when there is nothing to compare with. */
function change(now: number, before: number): number | undefined {
  if (!before) return undefined
  return (now - before) / before
}

function percent(x: number) {
  return `${(x * 100).toFixed(1)}%`
}

function KeyTable({
  title,
  rows,
  render,
  link = false,
}: {
  title: string
  rows: SearchKey[]
  render: (key: string) => string
  link?: boolean
}) {
  return (
    <section className="min-w-0">
      <h3 className="mb-2 text-sm font-semibold">{title}</h3>
      {rows.length === 0 ? (
        <p className="rounded-xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">{t.noRows}</p>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{link ? t.page : t.query}</TableHead>
                <TableHead className="text-right">{t.clicks}</TableHead>
                <TableHead className="text-right">{t.impressions}</TableHead>
                <TableHead className="text-right">{t.position}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.key}>
                  <TableCell className="max-w-72 truncate">
                    {link ? (
                      <a href={r.key} target="_blank" rel="noopener noreferrer" title={r.key} className="font-mono text-xs hover:underline">
                        {render(r.key)}
                      </a>
                    ) : (
                      <span title={r.key}>{render(r.key)}</span>
                    )}
                  </TableCell>
                  <TableCell className="tabular text-right">{formatInteger(r.totals.clicks)}</TableCell>
                  <TableCell className="tabular text-right">{formatInteger(r.totals.impressions)}</TableCell>
                  <TableCell className="tabular text-right">{r.totals.position ? r.totals.position.toFixed(1) : "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  )
}
