import { useEffect, useMemo, useRef } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { InboxIcon, Loader2Icon, PlusIcon } from "lucide-react"

import type { Job, Pipeline, Story } from "@/api/client"
import { EmptyState, ErrorState, PageHeader, ReadOnlyNote, TableSkeleton } from "@/components/page"
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
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { jobsQuery } from "@/queries/jobs"
import { keys } from "@/queries/keys"
import { pipelinesQuery } from "@/queries/pipelines"
import { STORY_LIST_LIMIT, storiesQuery } from "@/queries/stories"
import { DeliverableChip } from "./badges"
import { STORY_TABS, TAB_STATUSES, type StoryTab } from "./model"

const ROUTE = "/w/$workspaceId/stories"

/** Job kinds that turn into new stories when they finish. */
const PLANNING_KINDS = new Set(["story.adhoc", "pipeline.run"])
const POLL_PLANNING_MS = 5_000

export function StoriesPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const tab: StoryTab = search.tab ?? "review"
  const list = useQuery(storiesQuery(workspace.id, TAB_STATUSES[tab]))
  // Shares its cache with the nav badge.
  const review = useQuery(storiesQuery(workspace.id, TAB_STATUSES.review))
  const pipelines = useQuery(pipelinesQuery(workspace.id))
  const planning = usePlanningJobs(workspace.id)
  const now = useNow()

  const pipelineNames = useMemo(
    () => new Map((pipelines.data ?? []).map((p: Pipeline) => [p.id, p.definition.name])),
    [pipelines.data]
  )

  const newButton = canEdit ? (
    <Button
      render={<Link to="/w/$workspaceId/stories/new" params={{ workspaceId: workspace.id }} />}
      nativeButton={false}
    >
      <PlusIcon data-icon="inline-start" />
      {m.stories.newRequest}
    </Button>
  ) : null

  let body
  if (list.isPending) body = <TableSkeleton rows={5} />
  else if (list.isError) body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0) {
    const empty = m.stories.empty[tab]
    body = (
      <EmptyState
        icon={<InboxIcon />}
        title={empty.title}
        body={empty.body}
        action={tab === "review" || tab === "all" ? newButton : null}
      />
    )
  } else
    body = (
      <>
        <div className="rounded-3xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">{m.stories.columns.title}</TableHead>
                <TableHead>{m.stories.columns.outputs}</TableHead>
                <TableHead>{m.stories.columns.origin}</TableHead>
                <TableHead>{m.stories.columns.language}</TableHead>
                <TableHead className="pr-4">{m.stories.columns.created}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.data.map((s) => (
                <StoryRow
                  key={s.id}
                  workspaceId={workspace.id}
                  story={s}
                  pipelineName={s.pipelineId ? pipelineNames.get(s.pipelineId) : undefined}
                  pipelinesLoaded={!!pipelines.data}
                  timezone={workspace.timezone}
                  now={now}
                />
              ))}
            </TableBody>
          </Table>
        </div>
        {list.data.length >= STORY_LIST_LIMIT ? (
          <p className="mt-3 text-xs text-muted-foreground">{m.stories.showing(STORY_LIST_LIMIT)}</p>
        ) : null}
      </>
    )

  const reviewCount = review.data?.length ?? 0

  return (
    <>
      <PageHeader title={m.stories.title} description={m.stories.description} actions={newButton} />
      {canEdit ? null : <ReadOnlyNote>{m.stories.readOnlyHint}</ReadOnlyNote>}
      <Tabs
        value={tab}
        onValueChange={(next) =>
          void navigate({
            search: next === "review" ? {} : { tab: next as StoryTab },
            replace: true,
          })
        }
        className="mb-4"
      >
        <div className="-mx-1 overflow-x-auto px-1 pb-1">
          <TabsList aria-label={m.stories.title}>
            {STORY_TABS.map((t) => (
              <TabsTrigger key={t} value={t} className="flex-none">
                {m.stories.tabs[t]}
                {t === "review" && reviewCount > 0 ? (
                  <span className="rounded-full bg-amber-500/20 px-1.5 text-[11px] leading-4 font-semibold text-amber-800 tabular dark:text-amber-300">
                    {reviewCount >= STORY_LIST_LIMIT ? `${STORY_LIST_LIMIT}+` : reviewCount}
                  </span>
                ) : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>
      {planning.length > 0 && (tab === "progress" || tab === "all") ? (
        <PlanningNote count={planning.length} />
      ) : null}
      {body}
    </>
  )
}

/**
 * Queued or running jobs that will produce stories (ad-hoc requests and
 * pipeline runs). While there are any, the story lists refresh too, so
 * new stories show up without a reload.
 */
function usePlanningJobs(workspaceId: string): Job[] {
  const jobs = useQuery({
    ...jobsQuery(workspaceId, ["queued", "running"]),
    select: (all) => all.filter((j) => PLANNING_KINDS.has(j.kind)),
    refetchInterval: (q) =>
      q.state.data?.some((j) => PLANNING_KINDS.has(j.kind)) ? POLL_PLANNING_MS : false,
  })
  const planning = jobs.data ?? []
  const count = planning.length
  const qc = useQueryClient()
  const prev = useRef(count)
  useEffect(() => {
    // A planning job finished: the lists may have new stories.
    if (count < prev.current) void qc.invalidateQueries({ queryKey: keys.storyLists(workspaceId) })
    prev.current = count
  }, [count, qc, workspaceId])
  return planning
}

function PlanningNote({ count }: { count: number }) {
  return (
    <div
      role="status"
      className="mb-4 flex items-center gap-2.5 rounded-3xl border border-sky-500/30 bg-sky-500/5 px-4 py-3 text-sm"
    >
      <Loader2Icon className="size-4 shrink-0 animate-spin text-sky-600 dark:text-sky-400" />
      <div>
        <p className="font-medium">{m.stories.planning(count)}</p>
        <p className="text-muted-foreground">{m.stories.planningBody}</p>
      </div>
    </div>
  )
}

function StoryRow({
  workspaceId,
  story: s,
  pipelineName,
  pipelinesLoaded,
  timezone,
  now,
}: {
  workspaceId: string
  story: Story
  pipelineName: string | undefined
  pipelinesLoaded: boolean
  timezone: string
  now: number
}) {
  let origin: string
  if (!s.pipelineId) origin = m.stories.adhoc
  else if (pipelineName) origin = pipelineName
  else origin = pipelinesLoaded ? m.stories.deletedPipeline : m.common.none

  return (
    <TableRow>
      <TableCell className="max-w-md pl-4 align-top whitespace-normal">
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            to="/w/$workspaceId/stories/$storyId"
            params={{ workspaceId: workspaceId, storyId: s.id }}
            className="font-medium hover:underline"
          >
            {s.title}
          </Link>
          {s.angle ? (
            <span className="line-clamp-1 text-xs text-muted-foreground" title={s.angle}>
              {s.angle}
            </span>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="align-top">
        <ul className="grid gap-1">
          {s.deliverables.map((d) => (
            <li key={d.id}>
              <DeliverableChip deliverable={d} />
            </li>
          ))}
        </ul>
      </TableCell>
      <TableCell className="max-w-56 align-top">
        <div className="flex flex-col gap-0.5">
          {s.pipelineId && pipelineName ? (
            <Link
              to="/w/$workspaceId/pipelines/$pipelineId"
              params={{ workspaceId: workspaceId, pipelineId: s.pipelineId }}
              className="truncate text-sm hover:underline"
            >
              {origin}
            </Link>
          ) : (
            <span className="truncate text-sm">{origin}</span>
          )}
          {s.pipelineId ? (
            <span className="text-xs text-muted-foreground">{m.stories.origin[s.origin]}</span>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="align-top text-sm">
        {m.values.language[s.language] ?? s.language}
      </TableCell>
      <TableCell className="pr-4 align-top">
        <div className="flex flex-col gap-0.5">
          <span className="text-sm whitespace-nowrap tabular">{formatDateTime(s.createdAt, timezone)}</span>
          <span className="text-xs text-muted-foreground">{formatRelative(s.createdAt, now)}</span>
        </div>
      </TableCell>
    </TableRow>
  )
}
