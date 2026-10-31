import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  GlobeIcon,
  Loader2Icon,
  RefreshCwIcon,
  SettingsIcon,
  WrenchIcon,
} from "lucide-react"
import { toast } from "sonner"

import type { SiteOverview } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  PageHeader,
  ReadOnlyNote,
  TableSkeleton,
} from "@/components/page"
import { SearchField } from "@/components/search-field"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatAgo, formatDateTime, formatRelative, useNow } from "@/lib/time"
import { iconClass, toneClass, type BannerTone } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { crawlActive, siteQuery, useCrawlSite } from "@/queries/site"
import { ChangeDialog } from "./change-dialog"
import { FindingsView } from "./findings"
import { PagesTable } from "./pages-table"
import { FINDING_TABS, PAGE_CLASSES, type PageClassTab } from "./search"

const ROUTE = "/w/$workspaceId/site"
const t = m.site

export function SitePage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const canManage = useCan("manage")
  const query = useQuery(siteQuery(workspace.id))
  const crawl = useCrawlSite(workspace.id)
  const o = query.data
  const active = crawlActive(o)
  const [requesting, setRequesting] = useState(false)

  const settingsButton = (
    <Button
      variant="outline"
      render={
        <Link
          to="/w/$workspaceId/settings/site"
          params={{ workspaceId: workspace.id }}
        />
      }
      nativeButton={false}
    >
      <SettingsIcon data-icon="inline-start" />
      {o?.site ? t.settings : t.openSettings}
    </Button>
  )
  const start = async () => {
    try {
      await crawl.mutateAsync()
      toast.success(t.crawlStarted)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  let body
  if (query.isPending) body = <TableSkeleton rows={5} />
  else if (query.isError)
    body = (
      <ErrorState error={query.error} onRetry={() => void query.refetch()} />
    )
  else if (!o?.site)
    body = (
      <EmptyState
        icon={<GlobeIcon />}
        title={t.notSet}
        body={canManage ? t.notSetBody : t.notSetViewer}
        action={canManage ? settingsButton : null}
      />
    )
  else body = <SiteBody overview={o} />

  return (
    <>
      <PageHeader
        title={t.title}
        description={o?.site ? o.site.url : t.description}
        actions={
          o?.site ? (
            <>
              {settingsButton}
              {canEdit ? (
                <Button variant="outline" onClick={() => setRequesting(true)}>
                  <WrenchIcon data-icon="inline-start" />
                  {t.change.request}
                </Button>
              ) : null}
              {canEdit ? (
                <Button
                  disabled={active || crawl.isPending}
                  onClick={() => void start()}
                >
                  <RefreshCwIcon
                    data-icon="inline-start"
                    className={cn(active && "animate-spin")}
                  />
                  {t.crawlNow}
                </Button>
              ) : null}
            </>
          ) : null
        }
      />
      <ChangeDialog open={requesting} onOpenChange={setRequesting} />
      {o?.site && !canEdit ? (
        <ReadOnlyNote>{t.readOnlyHint}</ReadOnlyNote>
      ) : null}
      {body}
    </>
  )
}

function SiteBody({ overview: o }: { overview: SiteOverview }) {
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const view = search.view ?? "findings"
  const status = search.status ?? "open"
  const pageClass: PageClassTab = search.class ?? "all"
  const q = search.q ?? ""
  const root = o.site?.url ?? ""

  return (
    <div className="grid gap-6">
      <CrawlStatus overview={o} />
      {o.lastDoneCrawl ? (
        <section className="min-w-0">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
            <Tabs
              value={view}
              onValueChange={(next) =>
                void navigate({
                  search: next === "pages" ? { view: "pages" } : {},
                  replace: true,
                })
              }
            >
              <TabsList aria-label={t.title}>
                <TabsTrigger value="findings">{t.tabs.findings}</TabsTrigger>
                <TabsTrigger value="pages">{t.tabs.pages}</TabsTrigger>
              </TabsList>
            </Tabs>
            {view === "findings" ? (
              <Tabs
                value={status}
                onValueChange={(next) =>
                  void navigate({
                    search:
                      next === "open"
                        ? {}
                        : { status: next as "ignored" | "resolved" },
                    replace: true,
                  })
                }
              >
                <TabsList aria-label={t.tabs.findings}>
                  {FINDING_TABS.map((s) => (
                    <TabsTrigger key={s} value={s}>
                      {t.findingStatus[s]}
                    </TabsTrigger>
                  ))}
                </TabsList>
              </Tabs>
            ) : (
              <div className="flex flex-wrap items-center gap-3">
                <Tabs
                  value={pageClass}
                  onValueChange={(next) =>
                    void navigate({
                      search: (prev) => ({
                        view: "pages" as const,
                        q: prev.q,
                        class:
                          next === "all"
                            ? undefined
                            : (next as Exclude<PageClassTab, "all">),
                      }),
                      replace: true,
                    })
                  }
                >
                  <div className="-mx-1 overflow-x-auto px-1 pb-1">
                    <TabsList aria-label={t.tabs.pages}>
                      {PAGE_CLASSES.map((c) => (
                        <TabsTrigger key={c} value={c} className="flex-none">
                          {t.pageClass[c]}
                        </TabsTrigger>
                      ))}
                    </TabsList>
                  </div>
                </Tabs>
                <SearchField
                  value={q}
                  onChange={(next) =>
                    void navigate({
                      search: (prev) => ({ ...prev, q: next || undefined }),
                      replace: true,
                    })
                  }
                  label={t.search}
                  placeholder={t.searchPlaceholder}
                />
              </div>
            )}
          </div>
          {view === "findings" ? (
            <FindingsView overview={o} status={status} />
          ) : (
            <PagesTable
              root={root}
              crawl={o.lastDoneCrawl.id}
              pageClass={pageClass}
              q={q}
            />
          )}
        </section>
      ) : null}
    </div>
  )
}

/** Where the crawling stands, and what the last crawl learnt about the site. */
function CrawlStatus({ overview: o }: { overview: SiteOverview }) {
  const workspace = useWorkspace()
  const now = useNow()
  const latest = o.latestCrawl
  const done = o.lastDoneCrawl
  const site = o.site!
  const when = (iso: string | undefined) =>
    !iso
      ? ""
      : now - new Date(iso).getTime() < 60_000
        ? t.crawl.justNow
        : formatAgo(iso, now)

  let tone: BannerTone = "done"
  let title: string
  let detail: string | null = null
  if (latest?.status === "queued") {
    tone = "working"
    title = t.crawl.queued
    detail = t.crawl.runningBody
  } else if (latest?.status === "running") {
    tone = "working"
    title = t.crawl.running(latest.pages)
    detail = t.crawl.runningBody
  } else if (latest?.status === "failed") {
    tone = "danger"
    title = t.crawl.failed
    detail = latest.failure ?? null
  } else if (done) {
    // Neutral: the crawl went through, which says nothing of the findings.
    tone = "muted"
    title = t.crawl.done(done.pages, when(done.finishedAt))
  } else {
    tone = "muted"
    title = t.crawl.never
  }
  const next = site.nextCrawlAt
    ? t.crawl.next(formatRelative(site.nextCrawlAt, now))
    : t.crawl.scheduleOff

  const Icon =
    tone === "working"
      ? Loader2Icon
      : tone === "danger"
        ? AlertCircleIcon
        : CheckCircle2Icon
  const facts = done?.facts
  return (
    <div
      className={cn("rounded-xl border px-4 py-3.5 md:px-5", toneClass[tone])}
    >
      <div role="status" aria-live="polite" className="flex gap-3">
        <Icon
          className={cn(
            "mt-0.5 size-5 shrink-0",
            iconClass[tone],
            tone === "working" && "animate-spin"
          )}
        />
        <div className="min-w-0 space-y-0.5">
          <p
            className="font-medium"
            title={
              done?.finishedAt
                ? formatDateTime(done.finishedAt, workspace.timezone)
                : undefined
            }
          >
            {title}
          </p>
          <p className="text-sm break-words text-muted-foreground">
            {[
              detail,
              latest?.status === "failed" ? t.crawl.failedBody : null,
              facts?.truncated ? t.crawl.truncated(site.maxPages) : null,
              facts && facts.disallowed > 0
                ? t.facts.disallowed(facts.disallowed)
                : null,
              next,
            ]
              .filter(Boolean)
              .join(" ")}
          </p>
        </div>
      </div>
      {facts ? (
        <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-2 border-t pt-3 text-sm sm:grid-cols-4">
          <Fact
            label={t.facts.robots}
            ok={facts.robotsFound}
            value={facts.robotsFound ? t.facts.found : t.facts.missing}
          />
          <Fact
            label={t.facts.sitemap}
            ok={facts.sitemaps.length > 0}
            value={
              facts.sitemaps.length > 0
                ? t.facts.sitemapUrls(facts.sitemapUrls)
                : t.facts.missing
            }
          />
          <Fact
            label={t.facts.llms}
            ok={facts.llmsTxt}
            value={facts.llmsTxt ? t.facts.found : t.facts.missing}
          />
          <Fact
            label={t.facts.ai}
            ok={facts.aiBlocked.length === 0}
            value={
              facts.aiBlocked.length === 0
                ? t.facts.aiAllowed
                : t.facts.aiBlocked(facts.aiBlocked.length)
            }
            title={facts.aiBlocked.join(", ") || undefined}
          />
        </dl>
      ) : null}
    </div>
  )
}

function Fact({
  label,
  value,
  ok,
  title,
}: {
  label: string
  value: string
  ok: boolean
  title?: string
}) {
  return (
    <div className="min-w-0" title={title}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("font-medium", !ok && "text-destructive")}>{value}</dd>
    </div>
  )
}
