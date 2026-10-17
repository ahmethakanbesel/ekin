import type { ReactNode } from "react"
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  ChevronRightIcon,
  ClockIcon,
  Loader2Icon,
  XCircleIcon,
} from "lucide-react"

import type { Deliverable, Feedback, FeedbackNote, Story } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { iconClass, toneClass, type BannerTone } from "@/lib/tones"
import { cn } from "@/lib/utils"
import {
  failureSummary,
  formatTimestamp,
  scopeLabel,
  sourceOf,
} from "./model"

/** What is happening to the deliverable, in plain words. */
export function StatusBanner({
  story,
  deliverable: d,
  canEdit,
  timezone,
  className,
}: {
  story: Story
  deliverable: Deliverable
  canEdit: boolean
  timezone: string
  className?: string
}) {
  const now = useNow()
  const t = m.banner
  const when = (iso: string | undefined) =>
    iso ? `${formatRelative(iso, now)} (${formatDateTime(iso, timezone)})` : ""
  let tone: BannerTone
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
      title =
        d.revision === 0 ? t.queuedFirst : t.queuedRevision(d.revision + 1)
      body = d.pendingFeedback ? (
        <FeedbackNotes
          feedback={d.pendingFeedback}
          kind={d.kind}
          lang={story.language}
        />
      ) : (
        t.updates
      )
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
      body = <FailureBody deliverable={d} canEdit={canEdit} />
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
        "flex flex-wrap items-center justify-between gap-x-6 gap-y-3 rounded-xl border px-4 py-3.5 md:px-5",
        toneClass[tone],
        className
      )}
    >
      <div className="flex min-w-0 flex-1 basis-72 gap-3">
        <Icon
          className={cn(
            "mt-0.5 size-5 shrink-0",
            iconClass[tone],
            working && "animate-spin"
          )}
        />
        <div className="min-w-0 space-y-0.5">
          <p className="font-medium">{title}</p>
          {body ? (
            <div className="text-sm text-muted-foreground">{body}</div>
          ) : null}
        </div>
      </div>
    </div>
  )
}

/**
 * What failed in plain words, whether Retry is likely to help (for those
 * who can retry), and the raw error tucked away for whoever debugs it.
 */
function FailureBody({
  deliverable: d,
  canEdit,
}: {
  deliverable: Deliverable
  canEdit: boolean
}) {
  const { what, next } = failureSummary(d)
  return (
    <>
      <span className="block text-foreground/90">{what}</span>
      {canEdit ? <span className="mt-1 block">{next}</span> : null}
      <details className="group mt-2">
        <summary className="inline-flex cursor-pointer list-none items-center gap-1 rounded-sm text-xs font-medium text-foreground/80 outline-none select-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring [&::-webkit-details-marker]:hidden">
          <ChevronRightIcon
            className="size-3.5 transition-transform group-open:rotate-90"
            aria-hidden="true"
          />
          {m.banner.technicalDetails}
        </summary>
        <span className="mt-1.5 block max-h-40 overflow-y-auto font-mono text-xs whitespace-pre-wrap wrap-anywhere">
          {d.failure || m.banner.noFailure}
        </span>
      </details>
    </>
  )
}

/** The notes of one change request, one line each, in reading order. */
export function FeedbackNotes({
  feedback: f,
  kind,
  lang,
}: {
  feedback: Feedback
  kind: Deliverable["kind"]
  /** The notes' language: the story's, which reviewers write in. */
  lang: string
}) {
  if (f.notes.length === 1)
    return <FeedbackLine note={f.notes[0]} kind={kind} lang={lang} />
  return (
    <ul className="grid gap-2.5">
      {f.notes.map((n, i) => (
        <li key={i}>
          <FeedbackLine note={n} kind={kind} lang={lang} />
        </li>
      ))}
    </ul>
  )
}

/** One note: what it is about, where in the video, and what it says. */
export function FeedbackLine({
  note: f,
  kind,
  lang,
}: {
  note: FeedbackNote
  kind: Deliverable["kind"]
  /** The note's language: the story's, which reviewers write in. */
  lang: string
}) {
  return (
    <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
      <Badge variant="outline">
        {scopeLabel(f.scope, kind)}
      </Badge>
      {f.atSeconds !== undefined ? (
        <span className="tabular font-mono text-xs">
          @{formatTimestamp(f.atSeconds)}
        </span>
      ) : null}
      <span lang={lang} className="whitespace-pre-wrap text-foreground/90">
        “{f.text}”
      </span>
    </span>
  )
}
