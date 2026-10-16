import { useMemo, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronRightIcon, Trash2Icon } from "lucide-react"

import {
  isApiError,
  type Deliverable,
  type KnowledgeRef,
  type Story,
} from "@/api/client"
import { Banner, ErrorState, ReadOnlyNote } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { pipelineHue } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { documentsQuery } from "@/queries/documents"
import { StatusBadge } from "./badges"
import { deliverableLabel } from "./model"
import { FilesList, VideoDetails } from "./previews"
import { RevisionList } from "./revision-history"
import { StatusBanner } from "./status-alert"
import type { DeliverableReview } from "./use-deliverable-review"

/**
 * Beside the feed: where the deliverable stands, its revisions, its
 * metadata and script, and the story it belongs to.
 */
export function ReviewPanel({
  workspaceId,
  timezone,
  story,
  deliverable: d,
  review,
  canEdit,
  canManage,
  pipelineName,
  pipelinesLoaded,
  onDelete,
}: {
  workspaceId: string
  timezone: string
  story: Story
  deliverable: Deliverable
  review: DeliverableReview
  canEdit: boolean
  canManage: boolean
  pipelineName: string | undefined
  pipelinesLoaded: boolean
  onDelete: () => void
}) {
  const { current, latest, artifacts } = review

  return (
    <div className="grid">
      <Section className="space-y-4">
        <StatusBanner
          story={story}
          deliverable={d}
          canEdit={canEdit}
          timezone={timezone}
        />
        {review.publishError ? (
          <Banner
            title={m.review.publishFailed}
            action={
              canManage &&
              isApiError(review.publishError, "ValidationFailed") ? (
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
        {canEdit ? null : (
          <ReadOnlyNote className="mb-0">{m.story.readOnlyHint}</ReadOnlyNote>
        )}
        {review.viewingOld && current && latest ? (
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-dashed px-4 py-2.5 text-sm">
            <span className="text-muted-foreground">
              {m.revisions.viewingOld(current.number, latest.number)}
            </span>
            <Button size="sm" variant="ghost" onClick={() => review.pick(null)}>
              {m.revisions.showLatest}
            </Button>
          </div>
        ) : null}
      </Section>
      <Separator />
      <Section>
        {review.revisions.isError && !review.revisions.data ? (
          <ErrorState
            error={review.revisions.error}
            onRetry={() => void review.revisions.refetch()}
          />
        ) : (
          <RevisionList
            workspaceId={workspaceId}
            deliverable={d}
            revisions={review.list}
            loading={review.revisions.isPending && d.revision > 0}
            current={current?.number}
            latest={latest?.number}
            timezone={timezone}
            onPick={review.pick}
          />
        )}
      </Section>
      {current && artifacts ? (
        <>
          <Separator />
          <Section className="grid gap-6">
            {d.kind === "video" ? (
              <VideoDetails
                artifacts={artifacts}
                revision={current}
                videoRef={review.videoRef}
              />
            ) : null}
            <FilesList artifacts={artifacts} revision={current} />
          </Section>
        </>
      ) : null}
      <Separator />
      <Section title={m.feed.story}>
        <StoryFacts
          story={story}
          shown={d}
          workspaceId={workspaceId}
          timezone={timezone}
          pipelineName={pipelineName}
          pipelinesLoaded={pipelinesLoaded}
        />
      </Section>
      {canEdit ? (
        <>
          <Separator />
          <Section className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted-foreground">
              {m.feed.deleteHint}
            </p>
            <Button
              variant="outline"
              size="sm"
              className="text-destructive hover:text-destructive"
              onClick={onDelete}
            >
              <Trash2Icon data-icon="inline-start" />
              {m.story.delete}
            </Button>
          </Section>
        </>
      ) : null}
    </div>
  )
}

function Section({
  title,
  aside,
  className,
  children,
}: {
  title?: string
  aside?: ReactNode
  className?: string
  children: ReactNode
}) {
  return (
    <section className={cn("p-6 max-sm:px-4", className)}>
      {title ? (
        <div className="mb-4 flex items-baseline justify-between gap-2">
          <h2 className="font-semibold">{title}</h2>
          {aside ? (
            <span className="text-xs text-muted-foreground">{aside}</span>
          ) : null}
        </div>
      ) : null}
      {children}
    </section>
  )
}

/** Server messages are lower-case fragments ("connect a YouTube channel…"); show them as sentences. */
function sentence(s: string): string {
  const t = s.trim()
  if (!t) return t
  const first = t[0].toUpperCase() + t.slice(1)
  return /[.!?…]$/.test(first) ? first : `${first}.`
}

// ---- Story ----

function StoryFacts({
  story: s,
  shown,
  workspaceId,
  timezone,
  pipelineName,
  pipelinesLoaded,
}: {
  story: Story
  /** The deliverable on screen; the others are listed with their status. */
  shown: Deliverable
  workspaceId: string
  timezone: string
  pipelineName: string | undefined
  pipelinesLoaded: boolean
}) {
  const now = useNow()
  const hue = pipelineHue(s.pipelineId)
  const others = s.deliverables.filter((d) => d.id !== shown.id)

  let origin: ReactNode
  if (!s.pipelineId) origin = m.stories.origin.adhoc
  else if (pipelineName)
    origin = (
      <>
        <Link
          to="/w/$workspaceId/pipelines/$pipelineId"
          params={{ workspaceId, pipelineId: s.pipelineId }}
          className="rounded-sm font-medium outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring"
        >
          {pipelineName}
        </Link>
        <span className="text-muted-foreground">
          {" "}
          · {m.stories.origin[s.origin]}
        </span>
      </>
    )
  else origin = pipelinesLoaded ? m.stories.deletedPipeline : m.common.none

  return (
    <div className="grid gap-4">
      <dl className="grid grid-cols-[6.5rem_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-sm">
        <dt className="text-muted-foreground">{m.story.origin}</dt>
        <dd>
          <span
            className={cn(
              "mr-1.5 inline-block size-2 rounded-[2px] align-[0.05em]",
              hue.bg
            )}
            aria-hidden="true"
          />
          {origin}
        </dd>
        <dt className="text-muted-foreground">{m.story.language}</dt>
        <dd>{m.values.language[s.language] ?? s.language}</dd>
        {s.templateName ? (
          <>
            <dt className="text-muted-foreground">{m.story.template}</dt>
            <dd>{s.templateName}</dd>
          </>
        ) : null}
        {s.keyword ? (
          <>
            <dt className="text-muted-foreground">{m.story.keyword}</dt>
            <dd className="font-medium">{s.keyword}</dd>
          </>
        ) : null}
        <dt className="text-muted-foreground">{m.story.created}</dt>
        <dd>
          <span className="tabular">{formatDateTime(s.createdAt, timezone)}</span>
          <span className="text-muted-foreground">
            {" "}
            · {formatRelative(s.createdAt, now)}
          </span>
        </dd>
        {s.keywords.length > 0 ? (
          <>
            <dt className="text-muted-foreground">{m.story.keywords}</dt>
            <dd>
              <ul className="flex flex-wrap gap-1.5" aria-label={m.story.keywords}>
                {s.keywords.map((k) => (
                  <li key={k}>
                    <Badge variant="secondary" className="font-normal">
                      {k}
                    </Badge>
                  </li>
                ))}
              </ul>
            </dd>
          </>
        ) : null}
        {others.length > 0 ? (
          <>
            <dt className="text-muted-foreground">{m.feed.outputs}</dt>
            <dd>
              <ul className="grid gap-1.5">
                {others.map((d) => (
                  <li key={d.id} className="flex flex-wrap items-center gap-2">
                    <span>{deliverableLabel(d)}</span>
                    <StatusBadge status={d.status} revision={d.revision || undefined} />
                  </li>
                ))}
              </ul>
            </dd>
          </>
        ) : null}
      </dl>
      {s.instructions ? (
        <Disclosure title={m.story.instructions}>
          <p className="max-h-80 overflow-y-auto font-mono text-[13px] whitespace-pre-wrap">
            {s.instructions}
          </p>
        </Disclosure>
      ) : null}
      {s.refs?.length ? (
        <Disclosure title={m.story.refs(s.refs.length)}>
          <RefList workspaceId={workspaceId} refs={s.refs} />
        </Disclosure>
      ) : null}
    </div>
  )
}

function Disclosure({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <details className="group rounded-xl border">
      <summary className="flex cursor-pointer list-none items-center gap-2 rounded-xl px-4 py-2.5 text-sm font-medium outline-none select-none focus-visible:ring-[3px] focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
        <ChevronRightIcon className="size-4 text-muted-foreground transition-transform group-open:rotate-90" />
        {title}
      </summary>
      <div className="border-t px-4 py-3">{children}</div>
    </details>
  )
}

function RefList({
  workspaceId,
  refs,
}: {
  workspaceId: string
  refs: KnowledgeRef[]
}) {
  const documents = useQuery(documentsQuery(workspaceId))
  const byId = useMemo(
    () => new Map((documents.data ?? []).map((d) => [d.id.toLowerCase(), d])),
    [documents.data]
  )
  return (
    <ul className="grid gap-1.5 text-sm">
      {refs.map((r, i) => {
        const doc = byId.get(r.documentId.toLowerCase())
        const range =
          r.fromPage && r.toPage
            ? m.knowledge.viewer.rangeSummary(r.fromPage, r.toPage)
            : m.story.wholeDocument
        return (
          <li
            key={`${r.documentId}-${i}`}
            className="flex flex-wrap items-baseline gap-x-2"
          >
            {doc ? (
              <Link
                to="/w/$workspaceId/knowledge/$documentId"
                params={{ workspaceId, documentId: doc.id }}
                search={r.fromPage ? { page: r.fromPage } : {}}
                className="font-medium hover:underline"
              >
                {doc.title}
              </Link>
            ) : (
              <span className="text-muted-foreground">
                {m.pipelineForm.unknownDocument}
              </span>
            )}
            <span className="tabular text-xs text-muted-foreground">
              {range}
            </span>
          </li>
        )
      })}
    </ul>
  )
}
