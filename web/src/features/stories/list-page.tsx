import { useEffect, useMemo, useRef } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { InboxIcon, Loader2Icon, PlayIcon, PlusIcon } from "lucide-react"

import type { Job, Pipeline, Story } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  PageHeader,
  ReadOnlyNote,
} from "@/components/page"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { useNow } from "@/lib/time"
import { jobsQuery } from "@/queries/jobs"
import { keys } from "@/queries/keys"
import { pipelinesQuery } from "@/queries/pipelines"
import { STORY_LIST_LIMIT, storiesQuery } from "@/queries/stories"
import { STORY_TABS, TAB_STATUSES, type StoryTab } from "./model"
import { PosterCard } from "./poster-card"

const ROUTE = "/w/$workspaceId/stories"

/** Job kinds that turn into new stories when they finish. */
const PLANNING_KINDS = new Set(["story.adhoc", "pipeline.run"])
const POLL_PLANNING_MS = 5_000

/**
 * Stories per tab, counted from the unfiltered list (it shares its cache
 * with the All tab). A list cut off at the limit makes every count a floor.
 */
function useTabCounts(workspaceId: string, review: Story[] | undefined) {
  const all = useQuery(storiesQuery(workspaceId, TAB_STATUSES.all))
  return useMemo(() => {
    if (!all.data) return undefined
    const capped = all.data.length >= STORY_LIST_LIMIT
    const label = (n: number, cut = capped) => (cut ? `${n}+` : String(n))
    const counts = {} as Record<StoryTab, string>
    for (const tab of STORY_TABS) {
      const statuses = TAB_STATUSES[tab]
      const n =
        statuses.length === 0
          ? all.data.length
          : all.data.filter((s) => statuses.includes(s.status)).length
      counts[tab] = label(n)
    }
    // Its own query, so it stays exact while the All list is cut off.
    if (review)
      counts.review = label(review.length, review.length >= STORY_LIST_LIMIT)
    return counts
  }, [all.data, review])
}

export function StoriesPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const tab: StoryTab = search.tab ?? "review"
  const list = useQuery(storiesQuery(workspace.id, TAB_STATUSES[tab]))
  // Shares its cache with the nav badge.
  const review = useQuery(storiesQuery(workspace.id, TAB_STATUSES.review))
  const counts = useTabCounts(workspace.id, review.data)
  const pipelines = useQuery(pipelinesQuery(workspace.id))
  const planning = usePlanningJobs(workspace.id)
  const now = useNow()

  const pipelineNames = useMemo(
    () =>
      new Map(
        (pipelines.data ?? []).map((p: Pipeline) => [p.id, p.definition.name])
      ),
    [pipelines.data]
  )

  const firstReview = review.data?.[0]
  const newButton = canEdit ? (
    <Button
      variant={firstReview ? "outline" : "default"}
      render={
        <Link
          to="/w/$workspaceId/stories/new"
          params={{ workspaceId: workspace.id }}
        />
      }
      nativeButton={false}
    >
      <PlusIcon data-icon="inline-start" />
      {m.stories.newRequest}
    </Button>
  ) : null
  const sittingButton =
    canEdit && firstReview ? (
      <Button
        render={
          <Link
            to="/w/$workspaceId/stories/$storyId"
            params={{ workspaceId: workspace.id, storyId: firstReview.id }}
          />
        }
        nativeButton={false}
      >
        <PlayIcon data-icon="inline-start" />
        {m.stories.startSitting}
      </Button>
    ) : null

  let body
  if (list.isPending) body = <PosterSkeleton />
  else if (list.isError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
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
        <ul
          aria-label={m.stories.tabs[tab]}
          className="grid grid-cols-2 gap-x-4 gap-y-8 lg:grid-cols-4 lg:gap-x-6"
        >
          {list.data.map((s) => (
            <li key={s.id} className="min-w-0">
              <PosterCard
                workspaceId={workspace.id}
                story={s}
                pipelineName={
                  s.pipelineId ? pipelineNames.get(s.pipelineId) : undefined
                }
                pipelinesLoaded={!!pipelines.data}
                timezone={workspace.timezone}
                now={now}
              />
            </li>
          ))}
        </ul>
        {list.data.length >= STORY_LIST_LIMIT ? (
          <p className="mt-6 text-xs text-muted-foreground">
            {m.stories.showing(STORY_LIST_LIMIT)}
          </p>
        ) : null}
      </>
    )

  return (
    <>
      <PageHeader
        title={m.stories.title}
        description={m.stories.description}
        actions={
          newButton || sittingButton ? (
            <div className="flex flex-wrap items-center gap-2">
              {newButton}
              {sittingButton}
            </div>
          ) : null
        }
      />
      {canEdit ? null : <ReadOnlyNote>{m.stories.readOnlyHint}</ReadOnlyNote>}
      <Tabs
        value={tab}
        onValueChange={(next) =>
          void navigate({
            search: next === "review" ? {} : { tab: next as StoryTab },
            replace: true,
          })
        }
        className="mb-6"
      >
        <div className="scroll-area -mx-4 overflow-x-auto px-4 pb-1 md:mx-0 md:px-0">
          <TabsList aria-label={m.stories.title}>
            {STORY_TABS.map((t) => (
              <TabsTrigger key={t} value={t} className="flex-none">
                {m.stories.tabs[t]}
                {counts ? (
                  t === "review" && counts.review !== "0" ? (
                    <span className="tabular rounded-full bg-primary px-1.5 text-[11px] leading-4 font-semibold text-primary-foreground inset-ring inset-ring-primary-ink">
                      {counts.review}
                    </span>
                  ) : (
                    <span className="tabular text-xs text-muted-foreground">
                      {counts[t]}
                    </span>
                  )
                ) : null}
              </TabsTrigger>
            ))}
          </TabsList>
        </div>
      </Tabs>
      {planning.length > 0 && tab !== "done" && tab !== "failed" ? (
        <PlanningNote count={planning.length} />
      ) : null}
      {body}
    </>
  )
}

function PosterSkeleton() {
  return (
    <div
      className="grid grid-cols-2 gap-x-4 gap-y-8 lg:grid-cols-4 lg:gap-x-6"
      aria-busy="true"
      aria-label={m.common.loading}
    >
      {Array.from({ length: 4 }, (_, i) => (
        <div key={i} className="grid gap-3">
          <Skeleton className="aspect-[9/16] w-full rounded-xl" />
          <Skeleton className="h-4 w-3/4" />
          <Skeleton className="h-4 w-1/2" />
        </div>
      ))}
    </div>
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
    // The list holds whole requests: keep their planning jobs still at work.
    select: (all) =>
      all.filter(
        (j) =>
          PLANNING_KINDS.has(j.kind) &&
          (j.status === "queued" || j.status === "running")
      ),
    refetchInterval: (q) =>
      q.state.data?.some((j) => PLANNING_KINDS.has(j.kind))
        ? POLL_PLANNING_MS
        : false,
  })
  const planning = jobs.data ?? []
  const count = planning.length
  const qc = useQueryClient()
  const prev = useRef(count)
  useEffect(() => {
    // A planning job finished: the lists may have new stories.
    if (count < prev.current)
      void qc.invalidateQueries({ queryKey: keys.storyLists(workspaceId) })
    prev.current = count
  }, [count, qc, workspaceId])
  return planning
}

function PlanningNote({ count }: { count: number }) {
  return (
    <Alert role="status" className="mb-6 border-info/25 bg-info/10 *:[svg]:text-info">
      <Loader2Icon className="animate-spin" />
      <AlertTitle>{m.stories.planning(count)}</AlertTitle>
      <AlertDescription>{m.stories.planningBody}</AlertDescription>
    </Alert>
  )
}
