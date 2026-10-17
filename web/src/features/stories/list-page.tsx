import { useEffect, useMemo, useRef, useState } from "react"
import {
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import {
  InboxIcon,
  Loader2Icon,
  PlayIcon,
  PlusIcon,
  SearchIcon,
} from "lucide-react"

import type { Job, Pipeline, Story } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  LoadMore,
  PageHeader,
  ReadOnlyNote,
} from "@/components/page"
import { SearchField } from "@/components/search-field"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { jobRows, jobsQuery, type JobPages } from "@/queries/jobs"
import { keys } from "@/queries/keys"
import { pipelinesQuery } from "@/queries/pipelines"
import {
  storiesQuery,
  storyWindowQuery,
  windowCount,
  type StoryWindow,
} from "@/queries/stories"
import { BatchBar } from "./batch-bar"
import { decidable } from "./batch-actions"
import { STORY_TABS, TAB_STATUSES, type StoryTab } from "./model"
import { PosterCard } from "./poster-card"

const ROUTE = "/w/$workspaceId/stories"

/** Job kinds that turn into new stories when they finish. */
const PLANNING_KINDS = ["story.adhoc", "pipeline.run"]
const POLL_PLANNING_MS = 5_000

/**
 * Stories per tab, counted from the newest stories of any status (one
 * window, shared with the jobs page's names) rather than from the paged
 * lists. A window cut off at its limit makes every count a floor.
 */
function useTabCounts(workspaceId: string, review: StoryWindow | undefined) {
  const all = useQuery(storyWindowQuery(workspaceId, TAB_STATUSES.all))
  return useMemo(() => {
    if (!all.data) return undefined
    const recent = all.data
    const counts = {} as Record<StoryTab, string>
    for (const tab of STORY_TABS) {
      const statuses = TAB_STATUSES[tab]
      const n =
        statuses.length === 0
          ? recent.stories.length
          : recent.stories.filter((s) => statuses.includes(s.status)).length
      counts[tab] = windowCount(recent, n)
    }
    // Its own window, so it stays exact while the All window is cut off.
    if (review) counts.review = windowCount(review)
    return counts
  }, [all.data, review])
}

export function StoriesPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const tab: StoryTab = search.tab ?? "review"
  const q = search.q ?? ""
  const list = useInfiniteQuery(
    storiesQuery(workspace.id, { status: TAB_STATUSES[tab], q })
  )
  // The whole review queue (shared with the nav badge), not the paged tab.
  const review = useQuery(storyWindowQuery(workspace.id, TAB_STATUSES.review))
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

  // Stories picked for a batch decision, for this tab and search only.
  const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set())
  const [anchor, setAnchor] = useState<string | null>(null)
  const scope = `${tab}\n${q}`
  const [selectedIn, setSelectedIn] = useState(scope)
  if (selectedIn !== scope) {
    setSelectedIn(scope)
    setSelected(new Set())
    setAnchor(null)
  }
  const decidableStories = useMemo(
    () => (canEdit ? (list.data ?? []).filter(decidable) : []),
    [canEdit, list.data]
  )
  const chosen = decidableStories.filter((s) => selected.has(s.id))
  /** Toggles a story; with range, sets every story from the last one toggled to it alike. */
  const toggle = (s: Story, range: boolean) => {
    const next = new Set(selected)
    const on = !selected.has(s.id)
    const from = anchor ? decidableStories.findIndex((x) => x.id === anchor) : -1
    const to = decidableStories.findIndex((x) => x.id === s.id)
    if (range && from >= 0 && to >= 0) {
      for (const x of decidableStories.slice(Math.min(from, to), Math.max(from, to) + 1))
        if (on) next.add(x.id)
        else next.delete(x.id)
    } else if (on) next.add(s.id)
    else next.delete(s.id)
    setSelected(next)
    setAnchor(s.id)
  }

  const firstReview = review.data?.stories[0]
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
  else if (list.isLoadingError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0 && q)
    body = (
      <EmptyState
        icon={<SearchIcon />}
        title={m.common.noMatch(q)}
        body={m.common.noMatchBody}
      />
    )
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
          aria-busy={list.isPlaceholderData || undefined}
          className={cn(
            "grid grid-cols-2 gap-x-4 gap-y-8 transition-opacity lg:grid-cols-4 lg:gap-x-6",
            list.isPlaceholderData && "opacity-60"
          )}
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
                inReviewTab={tab === "review"}
                selection={
                  canEdit && decidable(s)
                    ? { selected: selected.has(s.id), onToggle: (range) => toggle(s, range) }
                    : undefined
                }
              />
            </li>
          ))}
        </ul>
        <LoadMore query={list} className={cn("pt-8", chosen.length > 0 && "pb-24")} />
        {chosen.length > 0 ? (
          <BatchBar
            workspaceId={workspace.id}
            chosen={chosen}
            selectable={decidableStories.length}
            onSelectAll={() => setSelected(new Set(decidableStories.map((s) => s.id)))}
            onSelect={(ids) => setSelected(new Set(ids))}
          />
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
      <div className="mb-6 flex flex-wrap items-center justify-between gap-x-4 gap-y-3">
        <Tabs
          value={tab}
          onValueChange={(next) =>
            void navigate({
              search: (prev) => ({
                q: prev.q,
                tab: next === "review" ? undefined : (next as StoryTab),
              }),
              replace: true,
            })
          }
          className="max-w-full min-w-0"
        >
          <div className="scroll-area -mx-4 min-w-0 overflow-x-auto px-4 pb-1 md:mx-0 md:px-0">
            <TabsList aria-label={m.stories.title}>
              {STORY_TABS.map((t) => (
                <TabsTrigger key={t} value={t} className="flex-none">
                  {m.stories.tabs[t]}
                  {counts ? (
                    t === "review" && counts.review !== "0" ? (
                      <span className="tabular rounded-full bg-primary px-1.5 text-xs leading-4 font-semibold text-primary-foreground inset-ring inset-ring-primary-ink">
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
        <SearchField
          value={q}
          onChange={(next) =>
            void navigate({
              search: (prev) => ({ ...prev, q: next || undefined }),
              replace: true,
            })
          }
          label={m.stories.search}
          placeholder={m.stories.searchPlaceholder}
        />
      </div>
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

/** The list holds whole requests: keep their planning jobs still at work. */
const planningJobs = (data: JobPages): Job[] =>
  jobRows(data).filter(
    (j) =>
      PLANNING_KINDS.includes(j.kind) &&
      (j.status === "queued" || j.status === "running")
  )

/**
 * Queued or running jobs that will produce stories (ad-hoc requests and
 * pipeline runs). While there are any, the story lists refresh too, so
 * new stories show up without a reload.
 */
function usePlanningJobs(workspaceId: string): Job[] {
  // The first page is enough: it counts requests, and nobody has dozens
  // of plans waiting at once.
  const jobs = useInfiniteQuery({
    ...jobsQuery(workspaceId, {
      status: ["queued", "running"],
      kind: PLANNING_KINDS,
    }),
    select: planningJobs,
    refetchInterval: (q) =>
      q.state.data?.pages.some((p) => p.jobs.length > 0)
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
