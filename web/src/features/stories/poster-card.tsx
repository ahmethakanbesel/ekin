import { useState, type CSSProperties } from "react"
import { Link } from "@tanstack/react-router"
import {
  FileTextIcon,
  MonitorIcon,
  SmartphoneIcon,
  SquareIcon,
  type LucideIcon,
} from "lucide-react"

import type { Deliverable, Story } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { m } from "@/i18n"
import { formatDateTime, formatRelative } from "@/lib/time"
import { pipelineHue } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { artifactUrl } from "@/queries/stories"
import { StatusBadge } from "./badges"
import { deliverableLabel, measuredLength, measuredWords } from "./model"

const orientationIcon: Record<string, LucideIcon> = {
  vertical: SmartphoneIcon,
  horizontal: MonitorIcon,
  square: SquareIcon,
}

/** The deliverable a card shows: the lead, else the first. */
function leadOf(story: Story): Deliverable | undefined {
  return story.deliverables.find((d) => d.role === "lead") ?? story.deliverables[0]
}

/**
 * A story in the review inbox: a 9:16 poster of its lead output (the
 * video's poster frame, or the article as a sheet of paper), then its
 * outputs, origin, language and age underneath.
 */
export function PosterCard({
  workspaceId,
  story: s,
  pipelineName,
  pipelinesLoaded,
  timezone,
  now,
}: {
  workspaceId: string
  story: Story
  pipelineName: string | undefined
  pipelinesLoaded: boolean
  timezone: string
  now: number
}) {
  const lead = leadOf(s)
  const hue = pipelineHue(s.pipelineId)
  const failed = s.deliverables.find((d) => d.status === "failed")

  let pipeline: string | undefined
  if (s.pipelineId)
    pipeline = pipelineName ?? (pipelinesLoaded ? m.stories.deletedPipeline : undefined)

  return (
    <article className="min-w-0" aria-labelledby={`story-${s.id}`}>
      <Link
        to="/w/$workspaceId/stories/$storyId"
        params={{ workspaceId, storyId: s.id }}
        className="group block rounded-xl outline-none focus-visible:ring-[3px] focus-visible:ring-ring"
      >
        {lead?.kind === "article" ? (
          <ArticlePoster story={s} lead={lead} />
        ) : (
          <VideoPoster
            workspaceId={workspaceId}
            story={s}
            lead={lead}
            hue={hue.color}
          />
        )}
      </Link>
      <div className="mt-3 grid gap-1.5 text-sm">
        {/* The poster carries the lead's status; the other outputs name theirs. */}
        <ul className="grid gap-0.5">
          {lead ? (
            <li className="font-medium">
              {deliverableLabel(lead)}
              {lead.revision > 0
                ? ` · ${m.stories.revisionShort(lead.revision)}`
                : null}
            </li>
          ) : null}
          {s.deliverables
            .filter((d) => d !== lead)
            .map((d) => (
              <li key={d.id} className="text-muted-foreground">
                {m.stories.otherOutput(deliverableLabel(d))}
                {d.revision > 0
                  ? ` · ${m.stories.revisionShort(d.revision)}`
                  : null}
                {` · ${m.stories.status[d.status] ?? d.status}`}
              </li>
            ))}
        </ul>
        {failed ? (
          <p className="text-destructive">
            <span className="font-medium">
              {m.banner.failed(m.banner.steps[failed.failedStep ?? ""] ?? m.banner.steps.unknown)}
            </span>
            {failed.failure ? (
              <span
                className="mt-0.5 line-clamp-2 text-xs text-muted-foreground"
                title={failed.failure}
              >
                {failed.failure}
              </span>
            ) : null}
          </p>
        ) : null}
        <p className="text-muted-foreground">
          <span
            className={cn("mr-1.5 inline-block size-2 rounded-[2px] align-[0.05em]", hue.bg)}
            aria-hidden="true"
          />
          {m.stories.origin[s.origin] ?? s.origin}
          {pipeline ? (
            <>
              {" · "}
              {pipelineName && s.pipelineId ? (
                <Link
                  to="/w/$workspaceId/pipelines/$pipelineId"
                  params={{ workspaceId, pipelineId: s.pipelineId }}
                  className="rounded-sm outline-none hover:text-foreground hover:underline focus-visible:ring-[3px] focus-visible:ring-ring"
                >
                  {pipeline}
                </Link>
              ) : (
                pipeline
              )}
            </>
          ) : null}
        </p>
        <p className="flex items-center gap-2">
          <Badge
            variant="outline"
            className="uppercase"
            title={m.values.language[s.language] ?? s.language}
          >
            {s.language}
          </Badge>
          <span
            className="text-xs text-muted-foreground"
            title={formatDateTime(s.createdAt, timezone)}
          >
            {formatRelative(s.createdAt, now)}
          </span>
        </p>
      </div>
    </article>
  )
}

/** Title and status over the foot of a poster. */
function PosterStrip({
  story,
  lead,
  title = true,
  className,
}: {
  story: Story
  lead: Deliverable | undefined
  title?: boolean
  className?: string
}) {
  return (
    <div className={cn("absolute inset-x-0 bottom-0 space-y-2 border-t p-3", className)}>
      {lead ? <StatusBadge status={lead.status} revision={lead.revision} /> : null}
      {title ? (
        <h2
          id={`story-${story.id}`}
          lang={story.language}
          className="line-clamp-3 text-sm leading-snug font-semibold"
        >
          {story.title}
        </h2>
      ) : null}
    </div>
  )
}

const frame =
  "relative aspect-[9/16] overflow-hidden rounded-xl border shadow-sm transition-shadow group-hover:shadow-md"

function VideoPoster({
  workspaceId,
  story,
  lead,
  hue,
}: {
  workspaceId: string
  story: Story
  lead: Deliverable | undefined
  hue: string
}) {
  const video = lead?.video
  const orientation = video?.orientation ?? "vertical"
  const length = measuredLength(lead?.measured)
  const Icon = orientationIcon[orientation] ?? SmartphoneIcon
  const src =
    lead && lead.revision > 0
      ? artifactUrl(workspaceId, story.id, lead.id, lead.revision, "poster.jpg")
      : undefined
  // The pipeline's hue, darkened, behind the frame and as the fallback.
  const style = {
    background: `radial-gradient(120% 70% at 70% 20%, color-mix(in oklab, ${hue} 55%, oklch(0.2 0.02 260)) 0%, color-mix(in oklab, ${hue} 22%, oklch(0.16 0.015 260)) 60%, oklch(0.14 0.01 260) 100%)`,
    containerType: "inline-size",
  } as CSSProperties

  return (
    <div className={cn("dark text-foreground", frame)} style={style}>
      {src ? (
        <PosterImage
          key={src}
          src={src}
          title={story.title}
          lang={story.language}
          className={
            orientation === "vertical"
              ? "absolute inset-0 size-full"
              : cn(
                  "absolute inset-x-0 top-[22%] w-full border-y",
                  orientation === "horizontal" ? "aspect-video" : "aspect-square"
                )
          }
        />
      ) : (
        <PosterTitle title={story.title} lang={story.language} />
      )}
      <div className="absolute inset-x-3 top-3 flex items-center justify-between gap-2">
        <Badge variant="secondary">
          <Icon data-icon="inline-start" />
          {m.stories.posterFormat(
            m.values.format[video?.format ?? "short"] ?? m.values.kind.video,
            m.stories.ratio[orientation] ?? orientation
          )}
          {length ? ` · ${length}` : null}
        </Badge>
      </div>
      <PosterStrip story={story} lead={lead} className="bg-background/85" />
    </div>
  )
}

/** The revision's poster frame; on a missing or broken image, the title instead. */
function PosterImage({
  src,
  title,
  lang,
  className,
}: {
  src: string
  title: string
  lang: string
  className: string
}) {
  const [broken, setBroken] = useState(false)
  if (broken) return <PosterTitle title={title} lang={lang} />
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setBroken(true)}
      className={cn("object-cover", className)}
    />
  )
}

function PosterTitle({ title, lang }: { title: string; lang: string }) {
  return (
    <p
      lang={lang}
      className="absolute inset-x-[9%] top-[24%] line-clamp-5 text-[clamp(1.1rem,9cqw,1.8rem)] leading-[1.1] font-semibold tracking-tight text-balance text-white"
      aria-hidden="true"
    >
      {title}
    </p>
  )
}

function ArticlePoster({ story, lead }: { story: Story; lead: Deliverable }) {
  return (
    <div className={cn("paper bg-card text-card-foreground", frame)}>
      <div className="absolute inset-x-3 top-3 flex items-center justify-between gap-2">
        <Badge variant="secondary">
          <FileTextIcon data-icon="inline-start" />
          {m.values.kind.article}
          {measuredWords(lead.measured) ? ` · ${measuredWords(lead.measured)}` : null}
        </Badge>
      </div>
      <div className="px-4 pt-14">
        <h2
          id={`story-${story.id}`}
          lang={story.language}
          className="line-clamp-5 text-xl leading-tight font-semibold tracking-tight text-balance"
        >
          {story.title}
        </h2>
        {story.angle ? (
          <p
            lang={story.language}
            className="mt-3 line-clamp-6 text-xs leading-relaxed text-muted-foreground"
          >
            {story.angle}
          </p>
        ) : null}
      </div>
      <PosterStrip story={story} lead={lead} title={false} className="bg-card" />
    </div>
  )
}
