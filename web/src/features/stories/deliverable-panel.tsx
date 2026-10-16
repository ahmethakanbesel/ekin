import { lazy, Suspense, type ReactNode } from "react"
import { Link } from "@tanstack/react-router"
import {
  CheckIcon,
  ExternalLinkIcon,
  Loader2Icon,
  MessageSquareIcon,
  PencilIcon,
  RotateCwIcon,
  SendIcon,
  XIcon,
} from "lucide-react"

import { isApiError, type Deliverable, type Story } from "@/api/client"
import { Banner, ErrorState } from "@/components/page"
import { Button, buttonVariants } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { ArticlePreview, FilesList, VideoPreview } from "./previews"
import { RejectDialog, RequestChangesDialog } from "./review-dialogs"
import { RevisionList } from "./revision-history"
import { StatusBanner } from "./status-alert"
import { useDeliverableReview } from "./use-deliverable-review"

// Only reviewers who edit need the editor; load it on first use.
const ContentEditor = lazy(() =>
  import("./content-editor").then((mod) => ({ default: mod.ContentEditor }))
)

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
  const review = useDeliverableReview({
    workspaceId,
    story,
    deliverable: d,
    canEdit,
  })
  const { revisions, list, latest, current, viewingOld, artifacts, busy } =
    review
  const showEditor = review.editing

  let actions: ReactNode = null
  if (canEdit && d.status === "review") {
    actions = (
      <>
        <Button
          disabled={busy}
          onClick={() => void review.approve()}
        >
          <CheckIcon data-icon="inline-start" />
          {m.review.approve}
        </Button>
        <Button variant="outline" disabled={busy} onClick={review.openChanges}>
          <MessageSquareIcon data-icon="inline-start" />
          {m.review.requestChanges}
        </Button>
        {d.revision > 0 ? (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => review.setEditing(true)}
          >
            <PencilIcon data-icon="inline-start" />
            {m.review.edit}
          </Button>
        ) : null}
        <Button
          variant="ghost"
          disabled={busy}
          onClick={() => review.setRejecting(true)}
        >
          <XIcon data-icon="inline-start" />
          {m.review.reject}
        </Button>
      </>
    )
  } else if (canEdit && d.status === "ready") {
    actions = (
      <Button
        disabled={busy}
        onClick={() => void review.publish()}
      >
        <SendIcon data-icon="inline-start" />
        {m.review.publish}
      </Button>
    )
  } else if (canEdit && d.status === "failed") {
    actions = (
      <>
        <Button
          disabled={busy}
          onClick={() => void review.retry()}
        >
          <RotateCwIcon data-icon="inline-start" />
          {m.review.retry}
        </Button>
        <Button
          variant="ghost"
          disabled={busy}
          onClick={() => review.setRejecting(true)}
        >
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

  let preview: ReactNode
  if (d.revision === 0) preview = <NoRevisionYet deliverable={d} />
  else if (revisions.isPending) preview = <PreviewSkeleton />
  else if (revisions.isError && !revisions.data)
    preview = (
      <ErrorState
        error={revisions.error}
        onRetry={() => void revisions.refetch()}
      />
    )
  else if (!current || !artifacts) preview = <NoRevisionYet deliverable={d} />
  else
    preview = (
      <div className="grid gap-6">
        {d.kind === "video" ? (
          <VideoPreview
            artifacts={artifacts}
            revision={current}
            deliverable={d}
            videoRef={review.videoRef}
          />
        ) : (
          <ArticlePreview artifacts={artifacts} revision={current} />
        )}
        <FilesList artifacts={artifacts} revision={current} />
      </div>
    )

  return (
    <div className="grid gap-5">
      <StatusBanner
        story={story}
        deliverable={d}
        canEdit={canEdit}
        timezone={timezone}
        actions={actions}
      />
      {review.publishError ? (
        <Banner
          title={m.review.publishFailed}
          action={
            canManage && isApiError(review.publishError, "ValidationFailed") ? (
              <Button
                size="sm"
                variant="outline"
                render={
                  <Link
                    to="/w/$workspaceId/settings"
                    params={{ workspaceId }}
                  />
                }
                nativeButton={false}
              >
                {m.review.openSettings}
              </Button>
            ) : null
          }
        >
          <p>{sentence(errorMessage(review.publishError))}</p>
        </Banner>
      ) : null}
      {showEditor ? (
        <Suspense fallback={<PreviewSkeleton />}>
          <ContentEditor
            workspaceId={workspaceId}
            story={story}
            deliverable={d}
            onClose={() => review.setEditing(false)}
            onDirtyChange={onDirtyChange}
          />
        </Suspense>
      ) : null}
      {!showEditor && viewingOld && current && latest ? (
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed px-4 py-2.5 text-sm">
          <span className="text-muted-foreground">
            {m.revisions.viewingOld(current.number, latest.number)}
          </span>
          <Button size="sm" variant="ghost" onClick={() => review.pick(null)}>
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
            onPick={review.pick}
          />
        </div>
      )}
      <RequestChangesDialog
        workspaceId={workspaceId}
        storyId={story.id}
        deliverable={review.changing ? d : null}
        atSeconds={review.changing?.atSeconds}
        onOpenChange={(open) => !open && review.closeChanges()}
      />
      <RejectDialog
        workspaceId={workspaceId}
        storyId={story.id}
        deliverable={review.rejecting ? d : null}
        onOpenChange={review.setRejecting}
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

function NoRevisionYet({ deliverable: d }: { deliverable: Deliverable }) {
  return (
    <div className="flex min-h-48 flex-col items-center justify-center gap-2 rounded-xl border border-dashed px-6 py-10 text-center text-sm text-muted-foreground">
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
      <Skeleton className="aspect-video w-full rounded-xl" />
      <Skeleton className="h-24 w-full" />
    </div>
  )
}
