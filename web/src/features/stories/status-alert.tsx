import type { ReactNode } from "react"
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  ClockIcon,
  Loader2Icon,
  XCircleIcon,
} from "lucide-react"

import type { Deliverable, Feedback, Story } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { iconClass, toneClass, type BannerTone } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { formatTimestamp, scopeLabel, sourceOf } from "./model"

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
        <FeedbackLine feedback={d.pendingFeedback} kind={d.kind} />
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

/** A piece of feedback: what it is about, where in the video, and what it says. */
export function FeedbackLine({
  feedback: f,
  kind,
}: {
  feedback: Feedback
  kind: Deliverable["kind"]
}) {
  return (
    <span className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
      <Badge variant="outline" className="h-4 px-1.5 text-[10px]">
        {scopeLabel(f.scope, kind)}
      </Badge>
      {f.atSeconds !== undefined ? (
        <span className="tabular font-mono text-xs">
          @{formatTimestamp(f.atSeconds)}
        </span>
      ) : null}
      <span className="whitespace-pre-wrap text-foreground/90">“{f.text}”</span>
    </span>
  )
}
