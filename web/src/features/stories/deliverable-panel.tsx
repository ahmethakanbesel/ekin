import { lazy, Suspense, useRef, useState, type ReactNode } from "react"
import { keepPreviousData, useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  CheckIcon,
  ClockIcon,
  ExternalLinkIcon,
  Loader2Icon,
  MessageSquareIcon,
  PencilIcon,
  RotateCwIcon,
  SendIcon,
  XCircleIcon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"

import { isApiError, type Deliverable, type Feedback, type Revision, type Story } from "@/api/client"
import { Banner, ErrorState } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button, buttonVariants } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { membersQuery } from "@/queries/members"
import { revisionsQuery, useDeliverableAction } from "@/queries/stories"
import { formatTimestamp, scopeLabel, sourceOf } from "./model"
import { ArticlePreview, FilesList, VideoPreview, type ArtifactRef } from "./previews"
import { RejectDialog, RequestChangesDialog } from "./review-dialogs"

// Only reviewers who edit need the editor; load it on first use.
const ContentEditor = lazy(() => import("./content-editor").then((mod) => ({ default: mod.ContentEditor })))

export function DeliverablePanel({
  workspaceId,
  timezone,
  story,
  deliverable: d,
  canEdit,
  canManage,
  onDirtyChange,
}: {
  workspaceId: string
  timezone: string
  story: Story
  deliverable: Deliverable
  canEdit: boolean
  canManage: boolean
  /** Whether the in-place editor holds unsaved changes. */
  onDirtyChange?: (dirty: boolean) => void
}) {
  const revisions = useQuery({
    ...revisionsQuery(workspaceId, story.id, d),
    enabled: d.revision > 0,
    // Keep showing the previous list while a new revision loads.
    placeholderData: keepPreviousData,
  })
  // null: follow the latest revision.
  const [picked, setPicked] = useState<number | null>(null)
  const videoRef = useRef<HTMLVideoElement>(null)
  const [changing, setChanging] = useState<{ atSeconds?: number } | null>(null)
  const [rejecting, setRejecting] = useState(false)
  const [editing, setEditing] = useState(false)
  // Editing applies to the latest revision of a deliverable in review only.
  const showEditor = editing && canEdit && d.status === "review" && d.revision > 0
  const approve = useDeliverableAction(workspaceId, story.id, "approve")
  const retry = useDeliverableAction(workspaceId, story.id, "retry")
  const publish = useDeliverableAction(workspaceId, story.id, "publish")

  const list = revisions.data ?? []
  const latest = list.at(-1)
  const current = (picked !== null ? list.find((r) => r.number === picked) : undefined) ?? latest
  const viewingOld = !!current && !!latest && current.number !== latest.number

  const run = async (
    action: { mutateAsync: (id: string) => Promise<Story> },
    success: string
  ): Promise<boolean> => {
    try {
      await action.mutateAsync(d.id)
      toast.success(success)
      return true
    } catch (err) {
      toast.error(errorMessage(err))
      return false
    }
  }

  const openChanges = () => {
    const el = videoRef.current
    let atSeconds: number | undefined
    if (d.kind === "video" && el && !viewingOld) {
      // Pause where the reviewer is, so the position they pin is the one they see.
      el.pause()
      atSeconds = el.currentTime
    }
    setChanging({ atSeconds })
  }

  const busy = approve.isPending || retry.isPending || publish.isPending

  let actions: ReactNode = null
  if (canEdit && d.status === "review") {
    actions = (
      <>
        <Button
          disabled={busy}
          onClick={() =>
            void run(approve, d.kind === "video" ? m.review.approvedVideo : m.review.approved)
          }
        >
          <CheckIcon data-icon="inline-start" />
          {m.review.approve}
        </Button>
        <Button variant="outline" disabled={busy} onClick={openChanges}>
          <MessageSquareIcon data-icon="inline-start" />
          {m.review.requestChanges}
        </Button>
        {d.revision > 0 ? (
          <Button variant="outline" disabled={busy} onClick={() => setEditing(true)}>
            <PencilIcon data-icon="inline-start" />
            {m.review.edit}
          </Button>
        ) : null}
        <Button variant="ghost" disabled={busy} onClick={() => setRejecting(true)}>
          <XIcon data-icon="inline-start" />
          {m.review.reject}
        </Button>
      </>
    )
  } else if (canEdit && d.status === "ready") {
    actions = (
      <Button disabled={busy} onClick={() => void run(publish, m.review.publishQueued)}>
        <SendIcon data-icon="inline-start" />
        {m.review.publish}
      </Button>
    )
  } else if (canEdit && d.status === "failed") {
    actions = (
      <>
        <Button disabled={busy} onClick={() => void run(retry, m.review.retried)}>
          <RotateCwIcon data-icon="inline-start" />
          {m.review.retry}
        </Button>
        <Button variant="ghost" disabled={busy} onClick={() => setRejecting(true)}>
          <XIcon data-icon="inline-start" />
          {m.review.reject}
        </Button>
      </>
    )
  }
  if (d.status === "published" && d.publication?.url) {
    actions = (
      <a
        href={d.publication.url}
        target="_blank"
        rel="noopener noreferrer"
        className={buttonVariants({ variant: "outline" })}
      >
        <ExternalLinkIcon data-icon="inline-start" />
        {m.review.openPublication}
      </a>
    )
  }

  // While editing, the editor has its own Save and Cancel.
  if (showEditor) actions = null

  const artifacts: ArtifactRef | undefined = current && {
    workspaceId,
    storyId: story.id,
    deliverableId: d.id,
    revision: current.number,
  }

  let preview: ReactNode
  if (d.revision === 0) preview = <NoRevisionYet deliverable={d} />
  else if (revisions.isPending) preview = <PreviewSkeleton />
  else if (revisions.isError && !revisions.data)
    preview = <ErrorState error={revisions.error} onRetry={() => void revisions.refetch()} />
  else if (!current || !artifacts) preview = <NoRevisionYet deliverable={d} />
  else
    preview = (
      <div className="grid gap-6">
        {d.kind === "video" ? (
          <VideoPreview artifacts={artifacts} revision={current} deliverable={d} videoRef={videoRef} />
        ) : (
          <ArticlePreview artifacts={artifacts} revision={current} />
        )}
        <FilesList artifacts={artifacts} revision={current} />
      </div>
    )

  return (
    <div className="grid gap-5">
      <StatusBanner story={story} deliverable={d} canEdit={canEdit} timezone={timezone} actions={actions} />
      {publish.error ? (
        <Banner
          title={m.review.publishFailed}
          action={
            canManage && isApiError(publish.error, "ValidationFailed") ? (
              <Button
                size="sm"
                variant="outline"
                render={<Link to="/w/$workspaceId/settings" params={{ workspaceId }} />}
                nativeButton={false}
              >
                {m.review.openSettings}
              </Button>
            ) : null
          }
        >
          <p>{sentence(errorMessage(publish.error))}</p>
        </Banner>
      ) : null}
      {showEditor ? (
        <Suspense fallback={<PreviewSkeleton />}>
          <ContentEditor
            workspaceId={workspaceId}
            story={story}
            deliverable={d}
            onClose={() => setEditing(false)}
            onDirtyChange={onDirtyChange}
          />
        </Suspense>
      ) : null}
      {!showEditor && viewingOld && latest ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-3xl border border-dashed px-4 py-2.5 text-sm">
          <span className="text-muted-foreground">
            {m.revisions.viewingOld(current.number, latest.number)}
          </span>
          <Button size="sm" variant="ghost" onClick={() => setPicked(null)}>
            {m.revisions.showLatest}
          </Button>
        </div>
      ) : null}
      {showEditor ? null : (
        <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
          <div className="min-w-0">{preview}</div>
          <RevisionList
            workspaceId={workspaceId}
            deliverable={d}
            revisions={list}
            loading={revisions.isPending && d.revision > 0}
            current={current?.number}
            latest={latest?.number}
            timezone={timezone}
            onPick={(n) => setPicked(n === latest?.number ? null : n)}
          />
        </div>
      )}
      <RequestChangesDialog
        workspaceId={workspaceId}
        storyId={story.id}
        deliverable={changing ? d : null}
        atSeconds={changing?.atSeconds}
        onOpenChange={(open) => !open && setChanging(null)}
      />
      <RejectDialog
        workspaceId={workspaceId}
        storyId={story.id}
        deliverable={rejecting ? d : null}
        onOpenChange={setRejecting}
      />
    </div>
  )
}

/** Server messages are lower-case fragments ("connect a YouTube channel…"); show them as sentences. */
function sentence(s: string): string {
  const t = s.trim()
  if (!t) return t
  const first = t[0].toUpperCase() + t.slice(1)
  return /[.!?…]$/.test(first) ? first : `${first}.`
}

// ---- Status banner ----

type Tone = "working" | "attention" | "done" | "danger" | "muted"

const toneClass: Record<Tone, string> = {
  working: "border-sky-500/30 bg-sky-500/5",
  attention: "border-amber-500/40 bg-amber-500/10",
  done: "border-emerald-500/30 bg-emerald-500/5",
  danger: "border-destructive/30 bg-destructive/5",
  muted: "bg-muted/40",
}

const iconClass: Record<Tone, string> = {
  working: "text-sky-600 dark:text-sky-400",
  attention: "text-amber-600 dark:text-amber-400",
  done: "text-emerald-600 dark:text-emerald-400",
  danger: "text-destructive",
  muted: "text-muted-foreground",
}

/** What is happening to the deliverable, in plain words, with what can be done about it. */
function StatusBanner({
  story,
  deliverable: d,
  canEdit,
  timezone,
  actions,
}: {
  story: Story
  deliverable: Deliverable
  canEdit: boolean
  timezone: string
  actions: ReactNode
}) {
  const now = useNow()
  const t = m.banner
  const when = (iso: string | undefined) =>
    iso ? `${formatRelative(iso, now)} (${formatDateTime(iso, timezone)})` : ""
  let tone: Tone
  let title: string
  let body: ReactNode = null

  switch (d.status) {
    case "waiting": {
      const source = sourceOf(story, d)
      const kind = (m.values.kind[source?.kind ?? "video"] ?? "").toLowerCase()
      tone = "muted"
      title = t.waiting(kind)
      body = t.waitingBody
      break
    }
    case "queued":
      tone = "working"
      title = d.revision === 0 ? t.queuedFirst : t.queuedRevision(d.revision + 1)
      body = d.pendingFeedback ? <FeedbackLine feedback={d.pendingFeedback} kind={d.kind} /> : t.updates
      break
    case "drafting":
      tone = "working"
      title =
        d.role === "translation"
          ? t.translating(d.language.toUpperCase())
          : t.drafting(d.revision + 1)
      body = t.draftingBody
      break
    case "rendering":
      tone = "working"
      title = t.rendering(d.revision)
      body = t.renderingBody
      break
    case "review":
      tone = "attention"
      title = t.review(d.revision)
      body = canEdit ? t.reviewBodyEditor : t.reviewBodyViewer
      break
    case "finalizing":
      tone = "working"
      title = t.finalizing
      body = t.finalizingBody
      break
    case "ready":
      tone = "done"
      title = t.ready
      body = d.approvedAt ? t.approvedAt(when(d.approvedAt)) : null
      break
    case "publishing":
      tone = "working"
      title = t.publishing
      body = t.updates
      break
    case "published":
      tone = "done"
      title = t.published
      body = d.publication ? (
        <>
          {t.publishedAt(when(d.publication.at))}{" "}
          <a
            href={d.publication.url}
            target="_blank"
            rel="noopener noreferrer"
            className="break-all text-foreground underline underline-offset-3"
          >
            {d.publication.url}
          </a>
        </>
      ) : null
      break
    case "rejected":
      tone = "muted"
      title = t.rejected
      body = t.rejectedBody
      break
    case "failed":
      tone = "danger"
      title = t.failed(t.steps[d.failedStep ?? ""] ?? t.steps.unknown)
      body = (
        <span className="block max-h-40 overflow-y-auto font-mono text-xs whitespace-pre-wrap">
          {d.failure || t.noFailure}
        </span>
      )
      break
  }

  const working = tone === "working"
  const Icon = working
    ? Loader2Icon
    : tone === "attention"
      ? ClockIcon
      : tone === "done"
        ? CheckCircle2Icon
        : tone === "danger"
          ? AlertCircleIcon
          : d.status === "rejected"
            ? XCircleIcon
            : ClockIcon

  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        "flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-3xl border px-4 py-3.5 md:px-5",
        toneClass[tone]
      )}
    >
      <div className="flex min-w-0 flex-1 basis-72 gap-3">
        <Icon className={cn("mt-0.5 size-5 shrink-0", iconClass[tone], working && "animate-spin")} />
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium">{title}</p>
          {body ? <div className="text-sm text-muted-foreground">{body}</div> : null}
        </div>
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}

function FeedbackLine({ feedback: f, kind }: { feedback: Feedback; kind: Deliverable["kind"] }) {
  return (
    <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
      <Badge variant="outline" className="h-4 px-1.5 text-[10px]">
        {scopeLabel(f.scope, kind)}
      </Badge>
      {f.atSeconds !== undefined ? (
        <span className="font-mono text-xs tabular">@{formatTimestamp(f.atSeconds)}</span>
      ) : null}
      <span className="whitespace-pre-wrap text-foreground/90">“{f.text}”</span>
    </span>
  )
}

// ---- Revisions ----

function RevisionList({
  workspaceId,
  deliverable: d,
  revisions,
  loading,
  current,
  latest,
  timezone,
  onPick,
}: {
  workspaceId: string
  deliverable: Deliverable
  revisions: Revision[]
  loading: boolean
  current: number | undefined
  latest: number | undefined
  timezone: string
  onPick: (n: number) => void
}) {
  const now = useNow()
  const newestFirst = revisions.toReversed()
  // Names for revisions a person edited in place (any member may list members).
  const members = useQuery({
    ...membersQuery(workspaceId),
    enabled: revisions.some((r) => r.editedBy),
  })
  const editorName = (userId: string) =>
    members.data?.find((x) => x.userId.toLowerCase() === userId.toLowerCase())?.name
  return (
    <aside className="grid gap-3 lg:sticky lg:top-4" aria-label={m.revisions.title}>
      <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
        {m.revisions.title}
      </h3>
      {d.pendingFeedback && d.status !== "review" ? (
        <div className="grid gap-1.5 rounded-3xl border border-dashed border-sky-500/40 p-3.5 text-sm">
          <p className="flex items-center gap-1.5 text-xs font-medium text-sky-700 dark:text-sky-300">
            <MessageSquareIcon className="size-3.5" />
            {m.revisions.pending(d.revision + 1)}
          </p>
          <FeedbackLine feedback={d.pendingFeedback} kind={d.kind} />
        </div>
      ) : null}
      {loading ? (
        <div className="grid gap-2">
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-16 w-full" />
        </div>
      ) : newestFirst.length === 0 ? (
        <p className="rounded-3xl border border-dashed px-4 py-6 text-center text-sm text-muted-foreground">
          {m.revisions.empty}
        </p>
      ) : (
        <ol className="grid gap-2">
          {newestFirst.map((r) => {
            const selected = r.number === current
            const editor = r.editedBy ? editorName(r.editedBy) : undefined
            return (
              <li key={r.number}>
                <button
                  type="button"
                  aria-pressed={selected}
                  onClick={() => onPick(r.number)}
                  className={cn(
                    "grid w-full gap-2 rounded-3xl border p-3.5 text-left text-sm transition-colors outline-none hover:bg-muted/50 focus-visible:ring-3 focus-visible:ring-ring/30",
                    selected && "border-primary/50 bg-primary/[0.04] hover:bg-primary/[0.06]"
                  )}
                >
                  <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <span className="font-medium">{m.revisions.revision(r.number)}</span>
                    {r.number === latest ? (
                      <Badge variant="secondary" className="h-4 px-1.5 text-[10px]">
                        {m.revisions.latest}
                      </Badge>
                    ) : null}
                    <span
                      className="ml-auto text-xs text-muted-foreground"
                      title={formatDateTime(r.createdAt, timezone)}
                    >
                      {formatRelative(r.createdAt, now)}
                    </span>
                  </span>
                  {r.feedback ? (
                    <span className="grid gap-1 rounded-2xl bg-muted/60 px-3 py-2 text-xs">
                      <span className="font-medium text-muted-foreground">{m.revisions.feedback}</span>
                      <FeedbackLine feedback={r.feedback} kind={d.kind} />
                    </span>
                  ) : null}
                  {r.editedBy ? (
                    <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                      <PencilIcon className="size-3.5" />
                      {editor ? m.revisions.editedBy(editor) : m.revisions.editedInPlace}
                    </span>
                  ) : (
                    <span className="grid gap-0.5 text-xs">
                      <span className="font-medium text-muted-foreground">{m.revisions.notes}</span>
                      <span
                        className={cn(
                          "whitespace-pre-wrap text-foreground/85",
                          !selected && "line-clamp-3"
                        )}
                      >
                        {r.notes.trim() || m.revisions.noNotes}
                      </span>
                    </span>
                  )}
                </button>
              </li>
            )
          })}
        </ol>
      )}
    </aside>
  )
}

function NoRevisionYet({ deliverable: d }: { deliverable: Deliverable }) {
  return (
    <div className="flex min-h-48 flex-col items-center justify-center gap-2 rounded-3xl border border-dashed px-6 py-10 text-center text-sm text-muted-foreground">
      {d.status === "drafting" || d.status === "queued" ? (
        <Loader2Icon className="size-5 animate-spin" />
      ) : null}
      {m.preview.nothingYet}
    </div>
  )
}

function PreviewSkeleton() {
  return (
    <div className="grid gap-4" aria-busy="true" aria-label={m.common.loading}>
      <Skeleton className="aspect-video w-full rounded-3xl" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
}
