import {
  lazy,
  Suspense,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  Link,
  useNavigate,
  useParams,
  useRouter,
} from "@tanstack/react-router"
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
import { storyQuery, storyWindowQuery } from "@/queries/stories"
import { ActionRail } from "./action-rail"
import { BackToStories } from "./back-link"
import { DiscardChangesDialog } from "./discard-dialog"
import type { FeedbackFocus } from "./feedback-form"
import { FeedbackSheet } from "./feedback-sheet"
import { TAB_STATUSES, deliverableLabel } from "./model"
import { seekVideo } from "./previews"
import { ReviewPanel } from "./review-panel"
import { recordDecision } from "./sitting-ledger"
import {
  ApproveWithNotesDialog,
  DeleteStoryDialog,
  PublishDialog,
  RejectDialog,
} from "./review-dialogs"
import {
  DraftFrame,
  FEED_END,
  FeedControls,
  FeedPreview,
  StoryFeed,
  TYPING,
  feedbackMarkers,
  useFeedNav,
} from "./story-feed"
import { notesToSend, useDeliverableReview } from "./use-deliverable-review"

// Only reviewers who edit need the editor; load it on first use.
const ContentEditor = lazy(() =>
  import("./content-editor").then((mod) => ({ default: mod.ContentEditor }))
)

const ROUTE = "/w/$workspaceId/stories/$storyId"

// Whether the reviewer keeps the docked feedback sheet open, for the
// browser session. A per-viewer convenience: storage may be unavailable.
const SHEET_KEY = "kurgu.feedback-sheet"
function sheetRemembered(): boolean {
  try {
    return window.sessionStorage.getItem(SHEET_KEY) === "open"
  } catch {
    return false
  }
}
function rememberSheet(open: boolean) {
  try {
    if (open) window.sessionStorage.setItem(SHEET_KEY, "open")
    else window.sessionStorage.removeItem(SHEET_KEY)
  } catch {
    // Not remembered; the sheet still works.
  }
}

export function StoryPage() {
  const workspace = useWorkspace()
  const { storyId } = useParams({ from: ROUTE })
  // The drafts waiting for review (shared with the inbox and its count).
  const review = useQuery(storyWindowQuery(workspace.id, TAB_STATUSES.review))
  const query = useQuery({
    ...storyQuery(workspace.id, storyId),
    // Moving through the feed shows the listed story while it loads.
    placeholderData: () => review.data?.stories.find((s) => s.id === storyId),
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
  return <StoryView story={query.data} reviewList={review.data?.stories} />
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
  const router = useRouter()
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

  // Notes written for the draft on screen and not sent yet. Typing does
  // not re-render the page, so this is read again when it matters.
  const unsentNow = () =>
    selected.status === "review" ? notesToSend(review.draft.get()) : 0
  const unsent = unsentNow()
  const [approvingWithNotes, setApprovingWithNotes] = useState(false)

  /**
   * Approving moves on at once, like any decision; the approval itself is
   * sent a few seconds later unless undone. Undo comes back to the draft
   * (and to any unsent notes, which are kept until then).
   */
  const approve = () => {
    const at = { story: story.id, id: selected.id }
    const started = review.approve(() => {
      const onStory = router.state.location.pathname.startsWith(
        `/w/${workspace.id}/stories/`
      )
      void navigate({
        to: ROUTE,
        params: { workspaceId: workspace.id, storyId: at.story },
        replace: onStory,
      })
      setPicked(at)
    })
    if (!started) return
    const waiting = story.deliverables.find(
      (d) => d.status === "review" && d.id !== selected.id
    )
    if (waiting) setPicked({ story: story.id, id: waiting.id })
    else if (sitting) nav.next()
  }

  const pick = (id: string) => {
    if (id === selected.id) return
    if (dirty) setPendingTab(id)
    else setPicked({ story: story.id, id })
  }

  // A waiting approval has decided the draft unless it is undone.
  const deciding = !!review.pendingApproval
  const canEditContent =
    canEdit &&
    selected.status === "review" &&
    selected.revision > 0 &&
    !deciding

  // The feedback sheet: docked from xl up, a sheet over the page below.
  // Only for a draft an editor can send back. It starts closed and opens
  // when there is something to say: Request changes, N, or notes still
  // unsent on the draft. Docked, it stays as the reviewer leaves it from
  // draft to draft, and for the rest of the browser session.
  const wide = useMediaQuery("(min-width: 1280px)")
  const reviewing = canEdit && selected.status === "review" && !deciding
  const [docked, setDockedState] = useState(sheetRemembered)
  const setDocked = (open: boolean) => {
    setDockedState(open)
    rememberSheet(open)
  }
  const [sheetOpen, setSheetOpen] = useState(false)
  const feedbackOpen = reviewing && (wide ? docked : sheetOpen)
  const setFeedbackOpen = wide ? setDocked : setSheetOpen
  const [focus, setFocus] = useState<FeedbackFocus | null>(null)
  // A request to focus the form belongs to the draft it was made on: the
  // next draft's form (or this one, remounted later) must not pin its time.
  const draftKey = `${story.id}/${selected.id}/${selected.revision}`
  const [focusFor, setFocusFor] = useState(draftKey)
  if (focusFor !== draftKey) {
    setFocusFor(draftKey)
    setFocus(null)
    // Notes left unsent on this draft are never hidden behind a closed sheet.
    if (unsent > 0) setDockedState(true)
  }
  const feedbackButton = useRef<HTMLButtonElement>(null)
  const openFeedback = (at?: number) => {
    setFeedbackOpen(true)
    setFocus((f) => ({ at, n: (f?.n ?? 0) + 1 }))
  }
  /** Request changes, and N: pause and write a note pinned where it stopped. */
  const pinNote = () => openFeedback(review.pause())

  // N pins a note at the playhead, unless a key belongs to a field or a
  // dialog is open.
  const pinRef = useRef(pinNote)
  useLayoutEffect(() => {
    pinRef.current = pinNote
  })
  const pinning = reviewing && !review.editing && !review.busy
  useEffect(() => {
    if (!pinning) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return
      if (e.key.toLowerCase() !== "n" || e.repeat) return
      const el = e.target instanceof Element ? e.target : null
      if (el?.closest(TYPING)) return
      if (document.querySelector('[role="dialog"], [role="alertdialog"]'))
        return
      e.preventDefault()
      pinRef.current()
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [pinning])

  // The docked sheet's column. It opens and closes with the sheet, and the
  // drafts, centred in the feed, slide with it. It stays while the sheet is
  // kept open but this draft has none (one being approved, one not in
  // review), so moving through the sitting never moves the drafts.
  const sheetColumn =
    wide &&
    docked &&
    canEdit &&
    !review.editing &&
    (sitting || selected.status === "review")

  return (
    <div
      className={cn(
        "min-w-0 xl:grid xl:h-[calc(100svh-3rem-1px)] xl:overflow-hidden motion-safe:xl:transition-[grid-template-columns] motion-safe:xl:duration-300 motion-safe:xl:ease-[cubic-bezier(0.16,1,0.3,1)]",
        sheetColumn
          ? "xl:grid-cols-[minmax(0,1fr)_340px_360px]"
          : "xl:grid-cols-[minmax(0,1fr)_0px_360px]"
      )}
    >
      <section
        aria-label={story.title}
        className="@container flex min-h-0 min-w-0 flex-col xl:col-start-1 xl:row-start-1"
      >
        {/* As tall with or without the outputs switch (38px), so the feed
            below keeps its height from story to story. */}
        <div className="flex flex-wrap items-center gap-2 px-4 py-3 sm:min-h-[62px]">
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
            <span className="@max-[640px]:sr-only">{m.feed.back}</span>
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
                  className="h-8 flex-1 gap-1.5 border sm:flex-none border-transparent px-2.5 font-normal text-muted-foreground hover:bg-background/60 hover:text-foreground aria-pressed:border-foreground/60 aria-pressed:bg-background aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:inset-ring-0"
                >
                  {deliverableLabel(d)}
                  {/* Hidden where the docked sheet leaves too little room
                      for the toolbar on one line; the panel's banner says it. */}
                  <span
                    className={cn(
                      "hidden text-xs font-normal sm:inline @max-[640px]:hidden",
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
                        ? feedbackMarkers(
                            selected,
                            review.list,
                            review.draft.get()
                          )
                        : []
                    }
                    onSeek={(t) => seekVideo(review.videoRef, t)}
                  />
                  <ActionRail
                    deliverable={selected}
                    review={review}
                    canEdit={canEdit}
                    onApprove={() =>
                      unsentNow() > 0 ? setApprovingWithNotes(true) : approve()
                    }
                    requestChanges={{
                      onClick: pinNote,
                      open: feedbackOpen,
                      unsent,
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
            // Said and sent: the sheet closes until there is more to say.
            setFeedbackOpen(false)
            onDecided(updated)
          }}
        />
      ) : null}
      <aside
        aria-label={m.feed.details}
        className="scroll-area min-h-0 border-t xl:col-start-3 xl:row-start-1 xl:overflow-y-auto xl:border-t-0 xl:border-l"
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
        unsent={unsent}
        onOpenChange={review.setRejecting}
        onRejected={(updated) => {
          recordDecision(story.id, selected.id, selected.revision, {
            kind: "rejected",
          })
          review.draft.clear()
          onDecided(updated)
        }}
      />
      <ApproveWithNotesDialog
        count={approvingWithNotes ? unsent : 0}
        revision={selected.revision}
        onOpenChange={setApprovingWithNotes}
        onApprove={approve}
      />
      <PublishDialog
        workspaceId={workspace.id}
        deliverable={
          review.confirmingPublish && selected.status === "ready"
            ? selected
            : null
        }
        busy={review.busy}
        onOpenChange={review.setConfirmingPublish}
        onConfirm={review.publish}
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
