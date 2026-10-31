import { useInfiniteQuery } from "@tanstack/react-query"
import { FileSearchIcon, SearchIcon } from "lucide-react"

import type { SitePage } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  LoadMore,
  TableSkeleton,
} from "@/components/page"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatInteger } from "@/lib/format"
import { badgeTone } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { sitePagesQuery } from "@/queries/site"
import { shortURL } from "./rules"
import type { PageClassTab } from "./search"

const t = m.site

const statusTone: Record<SitePage["class"], string> = {
  ok: badgeTone.success,
  redirect: badgeTone.neutral,
  error: badgeTone.danger,
}

/** The latest crawl's pages, by address. */
export function PagesTable({
  root,
  crawl,
  pageClass,
  q,
}: {
  root: string
  crawl: string
  pageClass: PageClassTab
  q: string
}) {
  const workspace = useWorkspace()
  const list = useInfiniteQuery(
    sitePagesQuery(workspace.id, { pageClass, q, crawl })
  )

  if (list.isPending) return <TableSkeleton rows={6} />
  if (list.isLoadingError)
    return <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  if (list.data.length === 0)
    return q ? (
      <EmptyState
        icon={<SearchIcon />}
        title={m.common.noMatch(q)}
        body={m.common.noMatchBody}
      />
    ) : (
      <EmptyState
        icon={<FileSearchIcon />}
        title={t.noPages.title}
        body={t.noPages.body}
      />
    )

  return (
    <>
      <div
        aria-busy={list.isPlaceholderData || undefined}
        className={cn(
          "rounded-xl border transition-opacity",
          list.isPlaceholderData && "opacity-60"
        )}
      >
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">{t.columns.page}</TableHead>
              <TableHead>{t.columns.status}</TableHead>
              <TableHead className="text-right">{t.columns.words}</TableHead>
              <TableHead>{t.columns.sitemap}</TableHead>
              <TableHead className="pr-4 text-right">
                {t.columns.issues}
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data.map((p) => (
              <TableRow key={p.url}>
                <TableCell className="max-w-md pl-4 align-top whitespace-normal">
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <a
                      href={p.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={p.url}
                      className="font-mono text-sm break-all underline-offset-3 hover:underline"
                    >
                      {shortURL(p.url, root)}
                    </a>
                    <span className="line-clamp-1 text-xs text-muted-foreground">
                      {p.redirectTo
                        ? t.redirectsTo(shortURL(p.redirectTo, root))
                        : p.status === 0
                          ? p.error || t.fetchFailed
                          : p.title || (p.class === "ok" ? t.noTitle : "")}
                    </span>
                  </div>
                </TableCell>
                <TableCell className="align-top">
                  <span className="flex flex-wrap items-center gap-1.5">
                    <span
                      className={cn(
                        "tabular inline-block min-w-9 rounded-full px-1.5 text-center text-xs leading-5 font-semibold",
                        statusTone[p.class]
                      )}
                    >
                      {p.status || "—"}
                    </span>
                    {p.noindex ? (
                      <Badge variant="outline">{t.noindex}</Badge>
                    ) : null}
                  </span>
                </TableCell>
                <TableCell className="tabular text-right align-top text-sm">
                  {p.class === "ok" ? formatInteger(p.words) : m.common.none}
                </TableCell>
                <TableCell className="align-top text-sm text-muted-foreground">
                  {p.inSitemap ? t.inSitemap : t.notInSitemap}
                </TableCell>
                <TableCell className="tabular pr-4 text-right align-top text-sm">
                  {p.openFindings > 0 ? (
                    <span className="font-medium">
                      {formatInteger(p.openFindings)}
                    </span>
                  ) : (
                    <span className="text-muted-foreground">0</span>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <LoadMore query={list} />
    </>
  )
}
