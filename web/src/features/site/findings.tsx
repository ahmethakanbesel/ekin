import { useState } from "react"
import { useInfiniteQuery } from "@tanstack/react-query"
import {
  ChevronRightIcon,
  CircleCheckIcon,
  EyeOffIcon,
  RotateCcwIcon,
} from "lucide-react"
import { toast } from "sonner"

import type { SiteFinding, SiteFindingStatus, SiteOverview } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  LoadMore,
  TableSkeleton,
} from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatInteger } from "@/lib/format"
import { formatAgo, formatDateTime, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { siteFindingsQuery, useSetFindingStatus } from "@/queries/site"
import {
  SEVERITIES,
  SITE_WIDE,
  ruleText,
  severityBadge,
  shortURL,
} from "./rules"

const t = m.site

/**
 * The open findings, grouped by severity and rule: each rule opens to
 * the pages it was raised on. Ignored and resolved findings are one list.
 */
export function FindingsView({
  overview,
  status,
}: {
  overview: SiteOverview
  status: SiteFindingStatus
}) {
  const crawl = overview.lastDoneCrawl?.id ?? ""
  if (status !== "open")
    return <FlatFindings status={status} overview={overview} crawl={crawl} />
  if (overview.open.length === 0)
    return (
      <EmptyState
        icon={<CircleCheckIcon />}
        title={t.noFindings.open.title}
        body={t.noFindings.open.body}
      />
    )
  return (
    <div className="grid gap-8">
      {SEVERITIES.map((sev) => {
        const rules = overview.open.filter((r) => r.severity === sev)
        if (rules.length === 0) return null
        const total = rules.reduce((n, r) => n + r.count, 0)
        return (
          <section key={sev} aria-labelledby={`severity-${sev}`}>
            <h3
              id={`severity-${sev}`}
              className="mb-3 flex items-center gap-2 text-sm font-semibold"
            >
              <span
                className={cn(
                  "tabular rounded-full px-2 text-xs leading-5",
                  severityBadge[sev]
                )}
              >
                {formatInteger(total)}
              </span>
              {t.severity[sev]}
            </h3>
            <ul className="divide-y rounded-xl border">
              {rules.map((r) => (
                <RuleGroup
                  key={r.rule}
                  rule={r.rule}
                  count={r.count}
                  overview={overview}
                  crawl={crawl}
                />
              ))}
            </ul>
          </section>
        )
      })}
    </div>
  )
}

function RuleGroup({
  rule,
  count,
  overview,
  crawl,
}: {
  rule: string
  count: number
  overview: SiteOverview
  crawl: string
}) {
  const text = ruleText(rule)
  // A closed <details> still mounts its children; load the pages only
  // once the rule is opened.
  const [open, setOpen] = useState(false)
  return (
    <li>
      <details
        className="group"
        onToggle={(e) => setOpen(e.currentTarget.open)}
      >
        <summary className="flex cursor-pointer list-none items-start gap-3 px-4 py-3 outline-none select-none hover:bg-muted/50 focus-visible:ring-[3px] focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
          <ChevronRightIcon
            className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90"
            aria-hidden="true"
          />
          <span className="min-w-0 flex-1">
            <span className="block font-medium">{text.title}</span>
            {text.why ? (
              <span className="mt-0.5 block text-sm text-muted-foreground">
                {text.why}
              </span>
            ) : null}
          </span>
          <span className="tabular shrink-0 text-sm text-muted-foreground">
            {SITE_WIDE.has(rule) ? t.siteWide : t.pagesCount(count)}
          </span>
        </summary>
        {open ? (
          <RuleFindings rule={rule} overview={overview} crawl={crawl} />
        ) : null}
      </details>
    </li>
  )
}

/** The open findings of one rule, a page at a time. */
function RuleFindings({
  rule,
  overview,
  crawl,
}: {
  rule: string
  overview: SiteOverview
  crawl: string
}) {
  const workspace = useWorkspace()
  const list = useInfiniteQuery(
    siteFindingsQuery(workspace.id, { status: "open", rule, crawl })
  )
  if (list.isPending)
    return (
      <div className="px-4 pb-4">
        <TableSkeleton rows={2} />
      </div>
    )
  if (list.isLoadingError)
    return (
      <div className="px-4 pb-4">
        <ErrorState error={list.error} onRetry={() => void list.refetch()} />
      </div>
    )
  return (
    <div className="border-t bg-muted/20 px-4 pt-1 pb-3">
      <ul className="divide-y">
        {list.data.map((f) => (
          <FindingRow key={f.id} finding={f} root={overview.site?.url} />
        ))}
      </ul>
      <LoadMore query={list} size="sm" className="pt-3" />
    </div>
  )
}

function FlatFindings({
  status,
  overview,
  crawl,
}: {
  status: SiteFindingStatus
  overview: SiteOverview
  crawl: string
}) {
  const workspace = useWorkspace()
  const list = useInfiniteQuery(
    siteFindingsQuery(workspace.id, { status, crawl })
  )
  if (list.isPending) return <TableSkeleton rows={4} />
  if (list.isLoadingError)
    return <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  if (list.data.length === 0) {
    const empty = t.noFindings[status]
    return (
      <EmptyState
        icon={<CircleCheckIcon />}
        title={empty.title}
        body={empty.body}
      />
    )
  }
  return (
    <>
      <ul className="divide-y rounded-xl border px-4">
        {list.data.map((f) => (
          <FindingRow
            key={f.id}
            finding={f}
            root={overview.site?.url}
            showRule
          />
        ))}
      </ul>
      <LoadMore query={list} />
    </>
  )
}

function FindingRow({
  finding: f,
  root,
  showRule = false,
}: {
  finding: SiteFinding
  root?: string
  showRule?: boolean
}) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const setStatus = useSetFindingStatus(workspace.id)
  const now = useNow()
  const siteWide = SITE_WIDE.has(f.rule)

  const change = async (status: "open" | "ignored") => {
    try {
      await setStatus.mutateAsync({ id: f.id, status })
      toast.success(status === "ignored" ? t.ignored : t.reopened)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <li className="flex items-start gap-3 py-2.5">
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          {showRule ? (
            <Badge variant="outline" className={severityBadge[f.severity]}>
              {ruleText(f.rule).title}
            </Badge>
          ) : null}
          {siteWide ? (
            <span className="text-sm font-medium">{t.siteWide}</span>
          ) : (
            <a
              href={f.url}
              target="_blank"
              rel="noopener noreferrer"
              title={f.url}
              className="min-w-0 font-mono text-sm break-all underline-offset-3 hover:underline"
            >
              {shortURL(f.url, root)}
            </a>
          )}
        </div>
        {f.evidence.length > 0 ? (
          <ul className="space-y-0.5 text-xs text-muted-foreground">
            {f.evidence.map((e, i) => (
              <li key={i} className="break-all">
                {e}
              </li>
            ))}
          </ul>
        ) : null}
        <p className="text-xs text-muted-foreground">
          {f.status === "resolved" && f.resolvedAt ? (
            <span title={formatDateTime(f.resolvedAt, workspace.timezone)}>
              {t.resolvedAt(formatAgo(f.resolvedAt, now))}
            </span>
          ) : (
            <span title={formatDateTime(f.firstSeen, workspace.timezone)}>
              {t.firstSeen(formatAgo(f.firstSeen, now))}
            </span>
          )}
        </p>
      </div>
      {canEdit && f.status === "open" ? (
        <Button
          variant="ghost"
          size="sm"
          disabled={setStatus.isPending}
          onClick={() => void change("ignored")}
        >
          <EyeOffIcon data-icon="inline-start" />
          {t.ignore}
        </Button>
      ) : null}
      {canEdit && f.status === "ignored" ? (
        <Button
          variant="ghost"
          size="sm"
          disabled={setStatus.isPending}
          onClick={() => void change("open")}
        >
          <RotateCcwIcon data-icon="inline-start" />
          {t.reopen}
        </Button>
      ) : null}
    </li>
  )
}
