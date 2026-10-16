import { lazy, Suspense, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import {
  ChevronLeftIcon,
  EllipsisIcon,
  PencilIcon,
  Trash2Icon,
} from "lucide-react"

import { isApiError, type Deliverable, type Story } from "@/api/client"
import { EmptyState, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { useMediaQuery } from "@/hooks/use-media-query"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { pipelinesQuery } from "@/queries/pipelines"
import { storiesQuery, storyQuery } from "@/queries/stories"
import { ActionRail } from "./action-rail"
import { BackToStories } from "./back-link"
import { DiscardChangesDialog } from "./discard-dialog"
import type { FeedbackFocus } from "./feedback-form"
import { FeedbackSheet } from "./feedback-sheet"
import { TAB_STATUSES, deliverableLabel } from "./model"
import { seekVideo } from "./previews"
import { ReviewPanel } from "./review-panel"
import { DeleteStoryDialog, RejectDialog } from "./review-dialogs"
import {
  DraftFrame,
  FEED_END,
  FeedControls,
  FeedPreview,
  StoryFeed,
  feedbackMarkers,
  useFeedNav,
} from "./story-feed"
import { useDeliverableReview } from "./use-deliverable-review"

// Only reviewers who edit need the editor; load it on first use.
const ContentEditor = lazy(() =>
  import("./content-editor").then((mod) => ({ default: mod.ContentEditor }))
)

const ROUTE = "/w/$workspaceId/stories/$storyId"

export function StoryPage() {
  const workspace = useWorkspace()
  const { storyId } = useParams({ from: ROUTE })
  // The drafts waiting for review (shared with the inbox and its count).
  const review = useQuery(storiesQuery(workspace.id, TAB_STATUSES.review))
  const query = useQuery({
    ...storyQuery(workspace.id, storyId),
    // Moving through the feed shows the listed story while it loads.
    placeholderData: () => review.data?.find((s) => s.id === storyId),
  })

  if (query.isPending) {
    return (
      <div className="grid gap-4 p-4 md:p-6">
        <Skeleton className="h-8 w-2/3" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="aspect-[9/16] w-full max-w-sm" />
      </div>
    )
  }
  if (query.isError) {
    if (
      isApiError(query.error) &&
      (query.error.status === 404 || query.error.status === 400)
    ) {
      return (
        <div className="max-w-5xl p-4 md:p-6">
          <BackToStories workspaceId={workspace.id} />
          <EmptyState title={m.story.notFound} />
        </div>
      )
    }
    return (
      <div className="p-4 md:p-6">
        <ErrorState error={query.error} onRetry={() => void query.refetch()} />
      </div>
    )
  }
  if (query.data.deliverables.length === 0) {
    return (
      <div className="max-w-5xl p-4 md:p-6">
        <BackToStories workspaceId={workspace.id} />
        <EmptyState title={m.story.noDeliverables} />
      </div>
    )
  }
  return <StoryView story={query.data} reviewList={review.data} />
}

/** The deliverable to open first: one waiting for review, else the first. */
function firstToOpen(story: Story): Deliverable | undefined {
  return (
    story.deliverables.find((d) => d.status === "review") ??
    story.deliverables[0]
  )
}

/**
 * The stories of a review sitting, in the order they were first listed.
 * A story keeps its place after it is decided, so the feed does not jump;
 * stories that arrive later join at the end. Null outside a sitting (the
 * story is not waiting for review).
 */
function sittingOrder(
  order: string[] | null,
  list: Story[] | undefined,
  currentId: string
): string[] | null {
  if (!list) return order
  if (!order || !order.includes(currentId)) {
    return list.some((s) => s.id === currentId) ? list.map((s) => s.id) : null
  }
  const added = list.filter((s) => !order.includes(s.id)).map((s) => s.id)
  return added.length > 0 ? [...order, ...added] : order
}

function StoryView({
  story,
  reviewList,
}: {
  story: Story
  reviewList: Story[] | undefined
}) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const canManage = useCan("manage")
  const navigate = useNavigate()
  const qc = useQueryClient()
  const [deleting, setDeleting] = useState(false)

  const [order, setOrder] = useState<string[] | null>(null)
  const nextOrder = sittingOrder(order, reviewList, story.id)
  if (nextOrder !== order) setOrder(nextOrder)
  const sitting = nextOrder !== null
  const listed = new Map((reviewList ?? []).map((s) => [s.id, s]))
  const stories = sitting
    ? nextOrder.flatMap((id) => {
        if (id === story.id) return [story]
        // A decided story has left the list; the cache has its new state.
        const s =
          listed.get(id) ??
          qc.getQueryData(storyQuery(workspace.id, id).queryKey)
        return s ? [s] : []
      })
    : [story]

  const pipelines = useQuery(pipelinesQuery(workspace.id))
  const pipelineName = (s: Story) =>
    s.pipelineId
      ? pipelines.data?.find((p) => p.id === s.pipelineId)?.definition.name
      : undefined

  // The output on screen, per story; the editor's unsaved changes guard
  // switching it (switching unmounts the editor).
  const [picked, setPicked] = useState<{ story: string; id: string } | null>(
    null
  )
  const [dirty, setDirty] = useState(false)
  const [pendingTab, setPendingTab] = useState<string | null>(null)
  const selected =
    (picked?.story === story.id
      ? story.deliverables.find((d) => d.id === picked.id)
      : undefined) ??
    firstToOpen(story) ??
    story.deliverables[0]

  const review = useDeliverableReview({
    workspaceId: workspace.id,
    story,
    deliverable: selected,
    canEdit,
  })

  const nav = useFeedNav({
    ids: sitting ? [...stories.map((s) => s.id), FEED_END] : [story.id],
    currentId: story.id,
    onCurrent: (id) =>
      void navigate({
        to: ROUTE,
        params: { workspaceId: workspace.id, storyId: id },
        replace: true,
      }),
    active: !review.editing && !dirty,
  })

  /** After a decision: stay on what still needs one, else move on. */
  const onDecided = (updated: Story) => {
    const waiting = updated.deliverables.find((d) => d.status === "review")
    if (waiting) setPicked({ story: updated.id, id: waiting.id })
    else if (sitting) nav.next()
  }

  const pick = (id: string) => {
    if (id === selected.id) return
    if (dirty) setPendingTab(id)
    else setPicked({ story: story.id, id })
  }

  const canEditContent =
    canEdit && selected.status === "review" && selected.revision > 0

  // The feedback sheet: docked from xl up (open unless closed), a sheet
  // over the page below. Only for a draft an editor can send back.
  const wide = useMediaQuery("(min-width: 1280px)")
  const reviewing = canEdit && selected.status === "review"
  const [docked, setDocked] = useState(true)
  const [sheetOpen, setSheetOpen] = useState(false)
  const feedbackOpen = reviewing && (wide ? docked : sheetOpen)
  const setFeedbackOpen = wide ? setDocked : setSheetOpen
  const [focus, setFocus] = useState<FeedbackFocus | null>(null)
  const feedbackButton = useRef<HTMLButtonElement>(null)
  const openFeedback = (at?: number) => {
    setFeedbackOpen(true)
    setFocus((f) => ({ at, n: (f?.n ?? 0) + 1 }))
  }

  return (
    <div
      className={cn(
        "min-w-0 xl:grid xl:h-[calc(100svh-3rem-1px)] xl:overflow-hidden",
        feedbackOpen
          ? "xl:grid-cols-[minmax(0,1fr)_340px_360px]"
          : "xl:grid-cols-[minmax(0,1fr)_360px]"
      )}
    >
      <section
        aria-label={story.title}
        className="flex min-h-0 min-w-0 flex-col"
      >
        <div className="flex flex-wrap items-center gap-2 px-4 py-3">
          <Button
            variant="ghost"
            size="sm"
            render={
              <Link
                to="/w/$workspaceId/stories"
                params={{ workspaceId: workspace.id }}
              />
            }
            nativeButton={false}
          >
            <ChevronLeftIcon data-icon="inline-start" />
            {m.feed.back}
          </Button>
          {story.deliverables.length > 1 ? (
            <ToggleGroup
              aria-label={m.story.deliverables}
              value={[selected.id]}
              onValueChange={(next) => next[0] && pick(String(next[0]))}
              spacing={1}
              className="order-last w-full rounded-lg bg-muted p-[3px] sm:order-none sm:mx-auto sm:w-auto"
            >
              {story.deliverables.map((d) => (
                <ToggleGroupItem
                  key={d.id}
                  value={d.id}
                  // Pressed: a foreground/60 border on the track (4.6:1 light, 5.2:1 dark).
                  className="h-8 flex-1 gap-1.5 border border-transparent px-2.5 font-normal text-muted-foreground hover:bg-background/60 hover:text-foreground aria-pressed:border-foreground/60 aria-pressed:bg-background aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:inset-ring-0"
                >
                  {deliverableLabel(d)}
                  <span
                    className={cn(
                      "hidden text-xs font-normal sm:inline",
                      d.status === "review"
                        ? "text-primary-ink"
                        : "text-muted-foreground"
                    )}
                  >
                    {d.revision > 0
                      ? `${m.stories.revisionShort(d.revision)} · `
                      : null}
                    {m.stories.status[d.status] ?? d.status}
                  </span>
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
          ) : null}
          <div
            className={cn(
              "ml-auto flex items-center gap-1",
              story.deliverables.length > 1 && "sm:ml-0"
            )}
          >
            {sitting ? (
              <FeedControls nav={nav} count={stories.length} />
            ) : null}
            {canEdit ? (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={m.feed.more}
                    />
                  }
                >
                  <EllipsisIcon />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="min-w-44">
                  {canEditContent ? (
                    <DropdownMenuItem onClick={() => review.setEditing(true)}>
                      <PencilIcon />
                      {m.review.edit}
                    </DropdownMenuItem>
                  ) : null}
                  <DropdownMenuItem
                    variant="destructive"
                    onClick={() => setDeleting(true)}
                  >
                    <Trash2Icon />
                    {m.story.delete}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : null}
          </div>
        </div>
        {review.editing ? (
          <div className="min-h-0 flex-1 px-4 pb-6 xl:overflow-y-auto">
            <Suspense fallback={<Skeleton className="h-[60vh] w-full" />}>
              <ContentEditor
                workspaceId={workspace.id}
                story={story}
                deliverable={selected}
                onClose={() => review.setEditing(false)}
                onDirtyChange={setDirty}
              />
            </Suspense>
          </div>
        ) : (
          <StoryFeed
            nav={nav}
            stories={stories}
            currentId={story.id}
            sitting={sitting}
            workspaceId={workspace.id}
            renderItem={(s, near) =>
              s.id === story.id ? (
                <div className="feed-stage">
                  <DraftFrame
                    workspaceId={workspace.id}
                    story={story}
                    deliverable={selected}
                    revision={review.current}
                    loading={review.revisions.isPending && selected.revision > 0}
                    current
                    player
                    videoRef={review.videoRef}
                    playback={review.playback}
                    pipelineName={pipelineName(story)}
                    markers={
                      selected.kind === "video"
                        ? feedbackMarkers(selected, review.list)
                        : []
                    }
                    onSeek={(t) => seekVideo(review.videoRef, t)}
                  />
                  <ActionRail
                    deliverable={selected}
                    review={review}
                    canEdit={canEdit}
                    onDecided={onDecided}
                    onRequestChanges={() => openFeedback(review.pause())}
                    feedback={{
                      open: feedbackOpen,
                      onToggle: () =>
                        feedbackOpen
                          ? setFeedbackOpen(false)
                          : openFeedback(),
                      ref: feedbackButton,
                    }}
                  />
                </div>
              ) : (
                <FeedPreview
                  workspaceId={workspace.id}
                  story={s}
                  deliverable={firstToOpen(s)}
                  near={near}
                  pipelineName={pipelineName(s)}
                />
              )
            }
          />
        )}
      </section>
      {reviewing && !review.editing ? (
        <FeedbackSheet
          docked={wide}
          workspaceId={workspace.id}
          story={story}
          deliverable={selected}
          revisions={review.list}
          playhead={review.playhead}
          focus={focus}
          draft={review.draft}
          open={feedbackOpen}
          onOpenChange={setFeedbackOpen}
          returnFocus={feedbackButton}
          onSent={(updated) => {
            setSheetOpen(false)
            onDecided(updated)
          }}
        />
      ) : null}
      <aside
        aria-label={m.feed.details}
        className="scroll-area min-h-0 border-t xl:overflow-y-auto xl:border-t-0 xl:border-l"
      >
        <ReviewPanel
          workspaceId={workspace.id}
          timezone={workspace.timezone}
          story={story}
          deliverable={selected}
          review={review}
          canEdit={canEdit}
          canManage={canManage}
          pipelineName={pipelineName(story)}
          pipelinesLoaded={!!pipelines.data}
          onDelete={() => setDeleting(true)}
        />
      </aside>

      <RejectDialog
        workspaceId={workspace.id}
        storyId={story.id}
        deliverable={review.rejecting ? selected : null}
        onOpenChange={review.setRejecting}
        onRejected={onDecided}
      />
      <DiscardChangesDialog
        open={pendingTab !== null}
        onKeep={() => setPendingTab(null)}
        onDiscard={() => {
          if (pendingTab) setPicked({ story: story.id, id: pendingTab })
          setPendingTab(null)
          setDirty(false)
        }}
      />
      <DeleteStoryDialog
        workspaceId={workspace.id}
        story={deleting ? story : null}
        onOpenChange={setDeleting}
        onDeleted={() =>
          void navigate({
            to: "/w/$workspaceId/stories",
            params: { workspaceId: workspace.id },
          })
        }
      />
    </div>
  )
}
