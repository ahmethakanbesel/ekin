import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import {
  ArrowUpRightIcon,
  RotateCcwIcon,
  SearchIcon,
  SparklesIcon,
  UploadIcon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"

import type { Opportunity } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  PageHeader,
  ReadOnlyNote,
  TableSkeleton,
} from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatInteger } from "@/lib/format"
import { formatAgo, formatDateTime, useNow } from "@/lib/time"
import { badgeTone } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { opportunitiesQuery, useSetOpportunityStatus } from "@/queries/seo"
import { ImportDialog } from "./import-dialog"
import { SeoProfileCard } from "./profile-card"
import { OpenSeoCard } from "./openseo-card"
import { RefreshDialog, useRefreshPlan } from "./refresh-dialog"
import { OPPORTUNITY_TABS, type OpportunityTab } from "./search"

const ROUTE = "/w/$workspaceId/seo"

export function SeoPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const tab: OpportunityTab = search.status ?? "open"
  const list = useQuery(opportunitiesQuery(workspace.id, tab))
  const now = useNow()
  const [importing, setImporting] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const plan = useRefreshPlan()
  const canManage = useCan("manage")

  const importButton = canEdit ? (
    <Button variant="outline" onClick={() => setImporting(true)}>
      <UploadIcon data-icon="inline-start" />
      {m.seo.import}
    </Button>
  ) : null

  let body
  if (list.isPending) body = <TableSkeleton rows={5} />
  else if (list.isError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0) {
    const empty = m.seo.empty[tab]
    body = (
      <EmptyState
        icon={<SearchIcon />}
        title={empty.title}
        body={empty.body}
        action={tab === "open" || tab === "all" ? importButton : null}
      />
    )
  } else
    body = (
      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">{m.seo.columns.keyword}</TableHead>
              {tab === "all" ? (
                <TableHead>{m.seo.columns.status}</TableHead>
              ) : null}
              <TableHead className="text-right" title={m.seo.volumeHint}>
                {m.seo.columns.volume}
              </TableHead>
              <TableHead className="text-right" title={m.seo.difficultyHint}>
                {m.seo.columns.difficulty}
              </TableHead>
              <TableHead>{m.seo.columns.intent}</TableHead>
              <TableHead className="text-right" title={m.seo.scoreHint}>
                {m.seo.columns.score}
              </TableHead>
              <TableHead>{m.seo.columns.source}</TableHead>
              <TableHead>{m.seo.columns.added}</TableHead>
              <TableHead className="w-12 pr-4">
                <span className="sr-only">{m.common.actions}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data.map((o) => (
              <OpportunityRow
                key={o.id}
                opportunity={o}
                showStatus={tab === "all"}
                canEdit={canEdit}
                now={now}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    )

  return (
    <>
      <PageHeader title={m.seo.title} description={m.seo.description} />
      <div className="grid items-start gap-8 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <section className="min-w-0">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <h2 className="font-heading text-lg font-semibold tracking-tight">
              {m.seo.opportunitiesTitle}
            </h2>
            {canEdit ? (
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" onClick={() => setRefreshing(true)}>
                  <SparklesIcon data-icon="inline-start" />
                  {m.seo.refresh}
                  {plan.credits ? (
                    <span className="tabular ml-1 rounded-full bg-muted px-1.5 text-xs font-normal text-muted-foreground">
                      {m.seo.estimateShort(formatInteger(plan.credits))}
                    </span>
                  ) : null}
                </Button>
                {importButton}
              </div>
            ) : null}
          </div>
          {canEdit ? null : <ReadOnlyNote>{m.seo.readOnlyHint}</ReadOnlyNote>}
          <Tabs
            value={tab}
            onValueChange={(next) =>
              void navigate({
                search:
                  next === "open"
                    ? {}
                    : { status: next as Exclude<OpportunityTab, "open"> },
                replace: true,
              })
            }
            className="mb-4"
          >
            <div className="-mx-1 overflow-x-auto px-1 pb-1">
              <TabsList aria-label={m.seo.opportunitiesTitle}>
                {OPPORTUNITY_TABS.map((t) => (
                  <TabsTrigger key={t} value={t} className="flex-none">
                    {m.seo.tabs[t]}
                  </TabsTrigger>
                ))}
              </TabsList>
            </div>
          </Tabs>
          {body}
        </section>
        <aside className="grid gap-6">
          {canManage ? <OpenSeoCard /> : null}
          <SeoProfileCard />
        </aside>
      </div>
      <ImportDialog open={importing} onOpenChange={setImporting} />
      <RefreshDialog open={refreshing} onOpenChange={setRefreshing} />
    </>
  )
}

function difficultyTone(d: number): string {
  if (d < 30) return badgeTone.success
  if (d < 60) return badgeTone.neutral
  return badgeTone.danger
}

const statusVariant = {
  open: "secondary",
  used: "outline",
  dismissed: "outline",
} as const

function OpportunityRow({
  opportunity: o,
  showStatus,
  canEdit,
  now,
}: {
  opportunity: Opportunity
  showStatus: boolean
  canEdit: boolean
  now: number
}) {
  const workspace = useWorkspace()
  const setStatus = useSetOpportunityStatus(workspace.id)

  const change = async (status: "open" | "dismissed") => {
    try {
      await setStatus.mutateAsync({ id: o.id, status })
      toast.success(
        status === "open"
          ? m.seo.reopened(o.keyword)
          : m.seo.dismissed(o.keyword)
      )
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  let action = null
  if (o.status === "used" && o.storyId)
    action = (
      <Button
        variant="ghost"
        size="sm"
        render={
          <Link
            to="/w/$workspaceId/stories/$storyId"
            params={{ workspaceId: workspace.id, storyId: o.storyId }}
          />
        }
        nativeButton={false}
      >
        {m.seo.openStory}
        <ArrowUpRightIcon data-icon="inline-end" />
      </Button>
    )
  else if (canEdit && o.status === "open")
    action = (
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={m.seo.dismiss}
        title={m.seo.dismiss}
        disabled={setStatus.isPending}
        onClick={() => void change("dismissed")}
      >
        <XIcon />
      </Button>
    )
  else if (canEdit && o.status === "dismissed")
    action = (
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label={m.seo.reopen}
        title={m.seo.reopen}
        disabled={setStatus.isPending}
        onClick={() => void change("open")}
      >
        <RotateCcwIcon />
      </Button>
    )

  return (
    <TableRow
      className={cn(o.status === "dismissed" && "text-muted-foreground")}
    >
      <TableCell className="max-w-sm pl-4 align-top whitespace-normal">
        <div className="flex min-w-0 flex-col gap-0.5">
          <span className="flex flex-wrap items-center gap-1.5">
            <span className="font-medium">{o.keyword}</span>
            <Badge
              variant="outline"
              className="uppercase"
              title={m.values.language[o.language]}
            >
              {o.language}
            </Badge>
          </span>
          {o.note ? (
            <span
              className="line-clamp-2 text-xs text-muted-foreground"
              title={o.note}
            >
              {o.note}
            </span>
          ) : null}
        </div>
      </TableCell>
      {showStatus ? (
        <TableCell className="align-top">
          <Badge variant={statusVariant[o.status]}>
            {m.seo.status[o.status]}
          </Badge>
        </TableCell>
      ) : null}
      <TableCell className="tabular text-right align-top">
        {o.volume > 0 ? formatInteger(o.volume) : m.common.none}
      </TableCell>
      <TableCell className="text-right align-top">
        {o.difficulty >= 0 ? (
          <span
            className={cn(
              "tabular inline-block min-w-8 rounded-full px-1.5 text-center text-xs leading-5 font-semibold",
              difficultyTone(o.difficulty)
            )}
          >
            {o.difficulty}
          </span>
        ) : (
          m.common.none
        )}
      </TableCell>
      <TableCell className="align-top text-sm capitalize">
        {o.intent || m.common.none}
      </TableCell>
      <TableCell
        className="tabular text-right align-top"
        title={m.seo.scoreHint}
      >
        {formatInteger(o.score)}
      </TableCell>
      <TableCell className="align-top text-sm">
        {m.seo.sources[o.source] ?? o.source}
      </TableCell>
      <TableCell className="align-top">
        <span
          className="text-sm whitespace-nowrap"
          title={formatDateTime(o.createdAt, workspace.timezone)}
        >
          {formatAgo(o.createdAt, now)}
        </span>
      </TableCell>
      <TableCell className="pr-4 text-right align-top">{action}</TableCell>
    </TableRow>
  )
}
