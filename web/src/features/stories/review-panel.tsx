import { useMemo, useSyncExternalStore, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Collapsible } from "@base-ui/react/collapsible"
import { ChevronRightIcon, Trash2Icon } from "lucide-react"

import {
  isApiError,
  type Deliverable,
  type FeedbackNote,
  type KnowledgeRef,
  type Revision,
  type Story,
} from "@/api/client"
import { Banner, ErrorState, ReadOnlyNote } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { useNarratorNames } from "@/features/voice/narrator"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { pipelineHue } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { allDocumentsQuery } from "@/queries/documents"
import { StatusBadge } from "./badges"
import {
  deliverableLabel,
  formatTimestamp,
  parseTimestamp,
  scopeLabel,
} from "./model"
import {
  FilesList,
  ScriptText,
  VideoDetails,
  seekVideo,
  videoFile,
  type ScriptNote,
} from "./previews"
import { RevisionHistory, historyCount } from "./revision-history"
import { ShortsPanel } from "./shorts-panel"
import { ThumbnailsPanel } from "./thumbnails-panel"
import { StatusBanner } from "./status-alert"
import type {
  DeliverableReview,
  FeedbackDraft,
} from "./use-deliverable-review"

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
  onPick,
  onThumbnailNote,
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
  /** Shows another deliverable of the story. */
  onPick: (deliverableId: string) => void
  /** Opens the feedback form on a note about the thumbnails, while notes can be sent. */
  onThumbnailNote?: () => void
}) {
  const { current, latest, artifacts } = review
  const language = m.values.language[story.language] ?? story.language
  const hasScript = !!current?.artifacts.includes("script.md")
  const nameNarrator = useNarratorNames(
    workspaceId,
    review.list.map((r) => r.measured?.narrator)
  )
  const narrator = nameNarrator(current?.measured?.narrator, d.language)

  return (
    <div className="grid grid-cols-[minmax(0,1fr)]">
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
        {review.list.length > 0 ? (
          <div className="flex flex-wrap items-center gap-3">
            <span id="panel-revision" className="text-sm font-medium">
              {m.panel.revision}
            </span>
            <ToggleGroup
              aria-labelledby="panel-revision"
              variant="outline"
              size="sm"
              spacing={0}
              className="max-w-full flex-wrap"
              value={current ? [String(current.number)] : []}
              onValueChange={(next) =>
                next[0] && review.pick(Number(next[0]))
              }
            >
              {review.list.map((r) => {
                const label = m.stories.revisionShort(r.number)
                // Selection stays neutral; the banner above says which one waits.
                return (
                  <ToggleGroupItem
                    key={r.number}
                    value={String(r.number)}
                    aria-label={m.feed.revisionItem(r.number)}
                    className="tabular aria-pressed:bg-secondary aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:inset-ring aria-pressed:inset-ring-foreground/60"
                  >
                    {r.number === latest?.number
                      ? m.panel.latest(label)
                      : label}
                  </ToggleGroupItem>
                )
              })}
            </ToggleGroup>
          </div>
        ) : null}
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
        {review.revisions.isError && !review.revisions.data ? (
          <ErrorState
            error={review.revisions.error}
            onRetry={() => void review.revisions.refetch()}
          />
        ) : null}
      </Section>
      {d.kind === "video" && current && artifacts && hasScript ? (
        <>
          <Separator />
          <Section
            name="script"
            title={m.panel.script}
            aside={
              narrator
                ? m.panel.readBy(narrator, language)
                : current.artifacts.includes("narration.wav")
                  ? m.panel.narration(language)
                  : language
            }
          >
            <ScriptText
              artifacts={artifacts}
              revision={current}
              lang={d.language}
              notes={scriptNotes(d, current, review.draft.get())}
              playback={videoFile(current) ? review.playback : undefined}
              onSeek={
                videoFile(current)
                  ? (t) => seekVideo(review.videoRef, t)
                  : undefined
              }
            />
          </Section>
        </>
      ) : null}
      {d.kind === "video" && current?.thumbnails && artifacts ? (
        <>
          <Separator />
          <Section
            name="thumbnails"
            title={m.thumbnails.title}
            aside={m.thumbnails.count(current.thumbnails.variants.length)}
          >
            <ThumbnailsPanel
              deliverable={d}
              revision={current}
              artifacts={artifacts}
              canEdit={canEdit}
              onNote={onThumbnailNote}
            />
          </Section>
        </>
      ) : null}
      {d.kind === "video" && d.video?.format === "long" && LIVE.includes(d.status) ? (
        <>
          <Separator />
          <Section name="shorts" title={m.shorts.title}>
            <ShortsPanel
              workspaceId={workspaceId}
              story={story}
              deliverable={d}
              canEdit={canEdit}
              onSeek={
                current && videoFile(current)
                  ? (t) => seekVideo(review.videoRef, t)
                  : undefined
              }
              onPick={onPick}
            />
          </Section>
        </>
      ) : null}
      {d.revision > 0 ? (
        <>
          <Separator />
          <Section
            id={HISTORY_ID}
            name="history"
            title={m.history.title}
            aside={
              review.revisions.data
                ? m.history.entries(historyCount(d, review.list))
                : undefined
            }
          >
            <RevisionHistory
              workspaceId={workspaceId}
              storyId={story.id}
              canEdit={canEdit}
              deliverable={d}
              revisions={review.list}
              loading={review.revisions.isPending}
              shown={current?.number}
              timezone={timezone}
              lang={story.language}
            />
          </Section>
        </>
      ) : null}
      {current && artifacts ? (
        <>
          <Separator />
          <Section name="details" title={m.panel.details}>
            <div className="grid gap-6">
              {d.kind === "video" ? (
                <VideoDetails
                  artifacts={artifacts}
                  revision={current}
                  lang={d.language}
                  videoRef={review.videoRef}
                />
              ) : null}
              <FilesList artifacts={artifacts} revision={current} />
            </div>
          </Section>
        </>
      ) : null}
      <Separator />
      <Section name="story" title={m.feed.story}>
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

/**
 * The notes to mark on a revision's script: those it answers, and on the
 * revision in review the notes being revised, the reviewer's unsent notes
 * and the one being written.
 */
function scriptNotes(
  d: Deliverable,
  shown: Revision,
  draft: FeedbackDraft
): ScriptNote[] {
  const out: ScriptNote[] = []
  const sent = (notes: FeedbackNote[] | undefined, on: number) => {
    for (const n of notes ?? [])
      if (n.atSeconds !== undefined)
        out.push({
          at: n.atSeconds,
          label: m.panel.noteOn(
            on,
            formatTimestamp(n.atSeconds),
            scopeLabel(n.scope, d.kind)
          ),
          text: n.text,
        })
  }
  sent(shown.feedback?.notes, shown.number - 1)
  if (shown.number !== d.revision) return out
  sent(d.pendingFeedback?.notes, d.revision)
  if (d.status !== "review") return out
  for (const n of draft.notes)
    if (n.at !== undefined)
      out.push({
        at: n.at,
        label: m.panel.unsentNote(
          formatTimestamp(n.at),
          scopeLabel(n.scope, d.kind)
        ),
        text: n.text,
        unsent: true,
      })
  const typed = parseTimestamp(draft.current.at)
  if (typed !== undefined)
    out.push({
      at: typed,
      label: m.panel.yourNote(formatTimestamp(typed)),
      unsent: true,
    })
  return out
}

/** The History section, which the feedback sheet points to for earlier notes. */
const HISTORY_ID = "panel-history"

/** A long video is cut into shorts once it is approved and live. */
const LIVE: Deliverable["status"][] = ["ready", "publishing", "published"]

/** Brings the panel's History into view, open, and moves focus to its heading. */
export function showHistory() {
  setCollapsed("history", false)
  // Opened on the next render; scroll once it has its height.
  requestAnimationFrame(() => {
    const el = document.getElementById(HISTORY_ID)
    if (!el) return
    const smooth = !window.matchMedia("(prefers-reduced-motion: reduce)")
      .matches
    el.scrollIntoView({ block: "start", behavior: smooth ? "smooth" : "auto" })
    el.querySelector<HTMLElement>("h2 button")?.focus({ preventScroll: true })
  })
}

// The sections the reviewer has collapsed, kept from draft to draft and
// across visits. A per-viewer convenience: storage may be unavailable.
const COLLAPSED_KEY = "kurgu.review-panel.collapsed"
let collapsed: ReadonlySet<string> = readCollapsed()
const listeners = new Set<() => void>()

function readCollapsed(): ReadonlySet<string> {
  try {
    const raw = window.localStorage.getItem(COLLAPSED_KEY)
    const names: unknown = raw ? JSON.parse(raw) : []
    return new Set(
      Array.isArray(names) ? names.filter((n) => typeof n === "string") : []
    )
  } catch {
    return new Set()
  }
}

function setCollapsed(name: string, closed: boolean) {
  if (collapsed.has(name) === closed) return
  const next = new Set(collapsed)
  if (closed) next.add(name)
  else next.delete(name)
  collapsed = next
  try {
    window.localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...next]))
  } catch {
    // Not remembered; the section still collapses.
  }
  for (const l of listeners) l()
}

function subscribeCollapsed(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

function useOpen(name: string | undefined): boolean {
  const set = useSyncExternalStore(subscribeCollapsed, () => collapsed)
  return !name || !set.has(name)
}

/**
 * A panel section. One with a name and a title collapses from its
 * heading; its contents stay findable with the browser's page search.
 */
function Section({
  id,
  name,
  title,
  aside,
  className,
  children,
}: {
  id?: string
  /** Remembers whether the section is collapsed. */
  name?: string
  title?: string
  aside?: ReactNode
  className?: string
  children: ReactNode
}) {
  const open = useOpen(name)
  if (!title || !name) {
    return (
      <section id={id} className={cn("p-6 max-sm:px-4", className)}>
        {title ? <h2 className="mb-4 font-semibold">{title}</h2> : null}
        {children}
      </section>
    )
  }
  return (
    <Collapsible.Root
      open={open}
      onOpenChange={(next) => setCollapsed(name, !next)}
      render={<section id={id} className="p-6 max-sm:px-4" />}
    >
      <div className="flex items-baseline justify-between gap-2">
        <h2 className="font-semibold">
          <Collapsible.Trigger className="group -mx-1 flex items-center gap-1.5 rounded-sm px-1 outline-none focus-visible:ring-[3px] focus-visible:ring-ring">
            {title}
            <ChevronRightIcon
              aria-hidden="true"
              className="size-4 text-muted-foreground group-data-panel-open:rotate-90 motion-safe:transition-transform"
            />
          </Collapsible.Trigger>
        </h2>
        {aside ? (
          <span className="text-xs text-muted-foreground">{aside}</span>
        ) : null}
      </div>
      {/* Padded out so the contents' focus rings are not clipped. */}
      <Collapsible.Panel
        hiddenUntilFound
        className="-mx-1 -mb-1 h-(--collapsible-panel-height) overflow-hidden px-1 pb-1 data-ending-style:h-0 data-starting-style:h-0 motion-safe:transition-[height] motion-safe:duration-200 motion-safe:ease-out"
      >
        <div className={cn("pt-4", className)}>{children}</div>
      </Collapsible.Panel>
    </Collapsible.Root>
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
            <dd lang={s.language} className="font-medium">
              {s.keyword}
            </dd>
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
                    <Badge
                      variant="secondary"
                      className="font-normal"
                      lang={s.language}
                    >
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
  const documents = useQuery(allDocumentsQuery(workspaceId))
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
