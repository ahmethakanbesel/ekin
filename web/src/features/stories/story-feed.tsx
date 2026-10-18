import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type ReactNode,
  type RefObject,
} from "react"
import { useQuery } from "@tanstack/react-query"
import {
  ChevronDownIcon,
  ChevronUpIcon,
  EyeIcon,
  FileTextIcon,
  FilmIcon,
  Loader2Icon,
} from "lucide-react"

import type { Deliverable, Feedback, Revision, Story } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Kbd } from "@/components/ui/kbd"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { m } from "@/i18n"
import { pipelineHue } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { artifactUrl, revisionsQuery } from "@/queries/stories"
import { StatusBadge } from "./badges"
import { SittingSummary } from "./sitting-summary"
import { PlayerBar, PlayerCorner, usePlayback, type PlaybackStore } from "./player"
import {
  formatTimestamp,
  measuredLength,
  measuredWords,
  parseTimestamp,
  scopeLabel,
} from "./model"
import {
  ArticlePreview,
  ScriptPreview,
  VideoFrameElement,
  videoFile,
  type ArtifactRef,
} from "./previews"
import type { FeedbackDraft } from "./use-deliverable-review"

/** The snap point after the last draft of a sitting. */
export const FEED_END = "__end"

const reducedMotion = () =>
  window.matchMedia("(prefers-reduced-motion: reduce)").matches

/** Keys typed here belong to the field, not to the feed. */
export const TYPING =
  'input, textarea, select, [contenteditable=""], [contenteditable="true"], [role="slider"], [role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]'

/**
 * Moving through the feed: J / K and the chevrons scroll to the next or
 * previous draft, and the draft that settles in view becomes the current
 * one (the page puts it in the URL). A programmatic scroll ignores the
 * drafts it passes, so the URL never bounces back.
 */
export function useFeedNav({
  ids,
  currentId,
  onCurrent,
  active,
}: {
  /** The drafts in order, ending with FEED_END in a sitting. */
  ids: string[]
  currentId: string
  onCurrent: (id: string) => void
  /** Whether J / K move the feed (not while editing). */
  active: boolean
}) {
  // State, not a ref: the feed unmounts while editing, and a new scroller
  // must be observed and scrolled to the current draft again.
  const [scroller, setScroller] = useState<HTMLElement | null>(null)
  const scrollerRef = useRef<HTMLElement | null>(null)
  useLayoutEffect(() => {
    scrollerRef.current = scroller
  })
  const target = useRef<string | null>(null)
  const timer = useRef(0)
  const [pos, setPos] = useState(currentId)
  const posRef = useRef(currentId)
  const currentRef = useRef(currentId)
  const onCurrentRef = useRef(onCurrent)
  useLayoutEffect(() => {
    currentRef.current = currentId
    onCurrentRef.current = onCurrent
  })

  const settle = useCallback((id: string) => {
    posRef.current = id
    setPos(id)
  }, [])

  /** Scrolls the draft to the top of the feed; the IO ignores what it passes. */
  const scrollTo = useCallback(
    (sc: HTMLElement | null, id: string, smooth: boolean) => {
      const el = sc?.querySelector<HTMLElement>(
        `[data-feed-item="${CSS.escape(id)}"]`
      )
      if (!sc || !el) return
      if (Math.abs(sc.scrollTop - el.offsetTop) < 2) return
      target.current = id
      window.clearTimeout(timer.current)
      timer.current = window.setTimeout(() => (target.current = null), 1500)
      sc.scrollTo({
        top: el.offsetTop,
        behavior: smooth && !reducedMotion() ? "smooth" : "auto",
      })
    },
    []
  )

  const goTo = (id: string) => {
    settle(id)
    if (id !== FEED_END && id !== currentRef.current) onCurrentRef.current(id)
    scrollTo(scrollerRef.current, id, true)
  }

  const index = Math.max(0, ids.indexOf(pos))
  const go = (delta: number) => {
    const next = ids[Math.min(ids.length - 1, Math.max(0, index + delta))]
    if (next && next !== pos) goTo(next)
  }
  const goRef = useRef(go)
  useLayoutEffect(() => {
    goRef.current = go
  })

  const key = ids.join(",")
  // On mount, and when drafts join or leave, keep the current one in view.
  useLayoutEffect(() => {
    if (posRef.current !== FEED_END || !key.includes(FEED_END)) {
      settle(currentRef.current)
      scrollTo(scroller, currentRef.current, false)
    }
  }, [key, scroller, settle, scrollTo])
  // A current draft chosen elsewhere (a link, a decision) scrolls into view.
  useLayoutEffect(() => {
    if (posRef.current === currentId) return
    settle(currentId)
    scrollTo(scrollerRef.current, currentId, false)
  }, [currentId, settle, scrollTo])

  useEffect(() => {
    const sc = scroller
    if (!sc) return
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (!e.isIntersecting || e.intersectionRatio < 0.6) continue
          const id = (e.target as HTMLElement).dataset.feedItem
          if (!id) continue
          if (target.current) {
            if (id !== target.current) continue
            target.current = null
          }
          if (id === posRef.current) continue
          settle(id)
          if (id !== FEED_END && id !== currentRef.current)
            onCurrentRef.current(id)
        }
      },
      { root: sc, threshold: 0.6 }
    )
    for (const id of key.split(",")) {
      const el = sc.querySelector(`[data-feed-item="${CSS.escape(id)}"]`)
      if (el) io.observe(el)
    }
    return () => io.disconnect()
  }, [key, scroller, settle])

  useEffect(() => {
    if (!active) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) return
      const k = e.key.toLowerCase()
      if (k !== "j" && k !== "k") return
      const el = e.target instanceof Element ? e.target : null
      if (el?.closest(TYPING)) return
      // A dialog or sheet is open over the feed.
      if (document.querySelector('[role="dialog"], [role="alertdialog"]'))
        return
      e.preventDefault()
      goRef.current(k === "j" ? 1 : -1)
    }
    document.addEventListener("keydown", onKey)
    return () => document.removeEventListener("keydown", onKey)
  }, [active])

  useEffect(() => () => window.clearTimeout(timer.current), [])

  return {
    /** The scroller's ref callback. */
    attach: setScroller,
    /** Index of the draft in view (FEED_END counts as past the last). */
    index,
    atEnd: pos === FEED_END,
    canPrev: index > 0,
    canNext: index < ids.length - 1,
    prev: () => go(-1),
    next: () => go(1),
  }
}

export type FeedNav = ReturnType<typeof useFeedNav>

/** ‹ 1 of N › with J / K hints. */
export function FeedControls({ nav, count }: { nav: FeedNav; count: number }) {
  const shown = Math.min(nav.index, count - 1) + 1
  return (
    <div className="flex items-center gap-1">
      <NavButton
        label={m.feed.previous}
        keyHint="K"
        disabled={!nav.canPrev}
        onClick={nav.prev}
      >
        <ChevronUpIcon />
      </NavButton>
      <span
        className="tabular min-w-12 text-center text-sm text-muted-foreground"
        aria-live="polite"
      >
        {m.feed.position(shown, count)}
      </span>
      <NavButton
        label={m.feed.next}
        keyHint="J"
        disabled={!nav.canNext}
        onClick={nav.next}
      >
        <ChevronDownIcon />
      </NavButton>
    </div>
  )
}

function NavButton({
  label,
  keyHint,
  disabled,
  onClick,
  children,
}: {
  label: string
  keyHint: string
  disabled: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={label}
            aria-keyshortcuts={keyHint}
            disabled={disabled}
            onClick={onClick}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent side="bottom">
        {label}
        <Kbd>{keyHint}</Kbd>
      </TooltipContent>
    </Tooltip>
  )
}

/** The scroller: one snap per story, and the end of the sitting after the last. */
export function StoryFeed({
  nav,
  stories,
  currentId,
  sitting,
  workspaceId,
  renderItem,
}: {
  nav: FeedNav
  stories: Story[]
  currentId: string
  sitting: boolean
  workspaceId: string
  /** A story's draft; `near` when it is next to the current one. */
  renderItem: (story: Story, near: boolean) => ReactNode
}) {
  const current = Math.max(
    0,
    stories.findIndex((s) => s.id === currentId)
  )
  const { attach } = nav
  return (
    <section
      ref={attach}
      className="feed outline-none focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:ring-inset"
      aria-label={sitting ? m.feed.label : m.feed.single}
    >
      {stories.map((s, i) => (
        <div
          key={s.id}
          data-feed-item={s.id}
          className="feed-item"
          aria-current={s.id === currentId ? "true" : undefined}
        >
          {renderItem(s, Math.abs(i - current) <= 1)}
        </div>
      ))}
      {sitting ? (
        // Centred like the drafts above it.
        <div data-feed-item={FEED_END} className="feed-item">
          <SittingSummary stories={stories} workspaceId={workspaceId} />
        </div>
      ) : null}
    </section>
  )
}

// ---- One draft ----

export type FeedMarker = {
  at: number
  /** The revision the feedback was given on. */
  revision: number
  scope: string
  text: string
  /** The reviewer's own note, not sent yet (being written when text is empty). */
  unsent?: boolean
}

/**
 * Feedback pinned to a time, from every revision and the pending request,
 * and on a draft in review the reviewer's unsent notes.
 */
export function feedbackMarkers(
  d: Deliverable,
  revisions: Revision[],
  draft?: FeedbackDraft
): FeedMarker[] {
  const out: FeedMarker[] = []
  const add = (f: Feedback | undefined, revision: number) => {
    for (const n of f?.notes ?? []) {
      if (n.atSeconds === undefined) continue
      out.push({
        at: n.atSeconds,
        revision,
        scope: scopeLabel(n.scope, d.kind),
        text: n.text,
      })
    }
  }
  for (const r of revisions) add(r.feedback, r.number - 1)
  add(d.pendingFeedback, d.revision)
  if (draft && d.status === "review") {
    for (const n of draft.notes)
      if (n.at !== undefined)
        out.push({
          at: n.at,
          revision: d.revision,
          scope: scopeLabel(n.scope, d.kind),
          text: n.text,
          unsent: true,
        })
    const typed = parseTimestamp(draft.current.at)
    if (typed !== undefined)
      out.push({
        at: typed,
        revision: d.revision,
        scope: scopeLabel(draft.current.scope, d.kind),
        text: "",
        unsent: true,
      })
  }
  return out.toSorted((a, b) => a.at - b.at)
}

/**
 * A story's draft in the feed: the video in a dark frame with a caption
 * strip, or the article on paper. `revision` is the one on screen; without
 * it (far from the current draft) only the poster shows.
 */
export function DraftFrame({
  workspaceId,
  story,
  deliverable: d,
  revision,
  loading,
  current,
  player,
  videoRef,
  playback,
  pipelineName,
  markers = [],
  onSeek,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable | undefined
  revision: Revision | undefined
  loading: boolean
  current: boolean
  /** Mount the player (the current draft and its neighbours). */
  player: boolean
  videoRef?: RefObject<HTMLVideoElement | null>
  /** The review's player state; with it (and the ref) the draft gets controls. */
  playback?: PlaybackStore
  pipelineName?: string
  markers?: FeedMarker[]
  onSeek?: (seconds: number) => void
}) {
  const Title = current ? "h1" : "h2"
  // The shown revision's measures; the latest's until it has loaded.
  const measured = revision ? revision.measured : d?.measured
  const artifacts: ArtifactRef | undefined =
    d && revision
      ? {
          workspaceId,
          storyId: story.id,
          deliverableId: d.id,
          revision: revision.number,
        }
      : undefined

  // A video's script waiting for approval reads as a page, like an article.
  const script =
    d?.kind === "video" &&
    (revision ? revision.stage === "script" : d.stage === "script" && d.revision > 0)
  if (d?.kind === "article" || script) {
    // Its own scroller: the wheel reads on and, at either end, a new
    // gesture moves the feed. Focused (click or Tab), Space and the arrow
    // keys scroll the article; J and K still move between drafts.
    return (
      <div
        role={current ? "region" : undefined}
        aria-label={
          current
            ? script
              ? m.feed.script(story.title)
              : m.feed.article(story.title)
            : undefined
        }
        tabIndex={current ? 0 : undefined}
        className="frame-paper paper scroll-area overflow-y-auto rounded-xl border bg-card text-card-foreground shadow-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring"
      >
        <div className="sticky top-0 z-10 flex items-center justify-between gap-2 border-b bg-card/95 px-5 py-3">
          <StatusBadge status={d.status} revision={d.revision} quiet />
          <Badge variant="outline">
            <FileTextIcon data-icon="inline-start" />
            {script ? m.feed.scriptKind : m.values.kind.article}
            {" · "}
            <span className="uppercase">{d.language}</span>
            {measuredWords(measured) ? ` · ${measuredWords(measured)}` : null}
          </Badge>
        </div>
        <div className="p-5 md:px-8 md:py-7">
          {player && artifacts && revision ? (
            script ? (
              <ScriptPreview
                artifacts={artifacts}
                revision={revision}
                lang={d.language}
              />
            ) : (
              <ArticlePreview
                artifacts={artifacts}
                revision={revision}
                lang={d.language}
              />
            )
          ) : loading ? (
            <FrameLoading />
          ) : (
            <div className="grid gap-3">
              <Title
                lang={story.language}
                className="text-2xl leading-tight font-semibold tracking-tight text-balance"
              >
                {story.title}
              </Title>
              {story.angle ? (
                <p lang={story.language} className="text-muted-foreground">
                  {story.angle}
                </p>
              ) : null}
              {d.revision === 0 ? <NothingYet deliverable={d} /> : null}
            </div>
          )}
        </div>
      </div>
    )
  }

  const orientation = d?.video?.orientation ?? "vertical"
  const shape =
    orientation === "horizontal"
      ? "frame-h"
      : orientation === "square"
        ? "frame-sq"
        : "frame-v"
  const file = revision ? videoFile(revision) : undefined
  const poster =
    d && d.revision > 0
      ? artifactUrl(
          workspaceId,
          story.id,
          d.id,
          revision?.number ?? d.revision,
          "poster.jpg"
        )
      : undefined
  const hue = pipelineHue(story.pipelineId)
  const backdrop = {
    background: `radial-gradient(120% 70% at 70% 20%, color-mix(in oklab, ${hue.color} 55%, oklch(0.2 0.02 260)) 0%, color-mix(in oklab, ${hue.color} 22%, oklch(0.16 0.015 260)) 60%, oklch(0.14 0.01 260) 100%)`,
  } as CSSProperties

  let screen: ReactNode
  if (player && file && artifacts && revision)
    screen = (
      <VideoFrameElement
        artifacts={artifacts}
        revision={revision}
        file={file}
        videoRef={videoRef}
        playback={playback}
        className="size-full object-contain"
      />
    )
  else if (loading) screen = <Skeleton className="size-full rounded-none" />
  else if (poster && (!revision || file))
    screen = (
      <PosterImage
        key={poster}
        src={poster}
        title={story.title}
        lang={story.language}
      />
    )
  else
    screen = (
      <div className="flex size-full flex-col items-center justify-center gap-2 p-6 text-center text-sm text-muted-foreground">
        {d && (d.status === "drafting" || d.status === "queued" || d.status === "rendering") ? (
          <Loader2Icon className="size-5 animate-spin" />
        ) : (
          <FilmIcon className="size-6" />
        )}
        {!d || d.revision === 0
          ? m.preview.nothingYet
          : d.status === "rendering" && revision?.number === d.revision
            ? m.preview.rendering
            : m.preview.noVideo}
      </div>
    )

  // A 9:16 frame fills the column; its caption strip lies over its foot.
  const overlay = orientation === "vertical"
  const controls = !!(player && file && playback && videoRef)
  // The times listed beside the scrubber are the notes the agent has.
  const sent = markers.filter((k) => !k.unsent)
  const timed = sent.length > 0 && onSeek
  return (
    <div
      data-player
      className={cn(
        "dark relative flex flex-col overflow-hidden rounded-xl border bg-card text-foreground shadow-sm",
        shape
      )}
    >
      <div
        className={cn(
          "relative min-h-0 bg-black",
          overlay
            ? "flex-1"
            : orientation === "horizontal"
              ? "aspect-video"
              : "aspect-square"
        )}
        style={file && player ? undefined : backdrop}
      >
        {screen}
        {file ? (
          <Badge
            variant="secondary"
            className="pointer-events-none absolute top-3 left-3 bg-background/80"
          >
            <EyeIcon data-icon="inline-start" />
            {file === "final.mp4" ? m.feed.final : m.feed.preview}
          </Badge>
        ) : null}
        {controls && playback && videoRef ? (
          <PlayerCorner
            videoRef={videoRef}
            className="absolute top-3 right-3"
          />
        ) : null}
      </div>
      <div
        className={cn(
          "grid gap-1 border-t p-4 max-sm:p-3",
          overlay
            ? "absolute inset-x-0 bottom-0 bg-background/85"
            : "bg-background"
        )}
      >
        <Caption
          playback={controls && overlay ? playback : undefined}
        >
          <div className="flex flex-wrap items-center gap-1.5">
            {d ? (
              <StatusBadge status={d.status} revision={d.revision} quiet />
            ) : null}
            {d?.video ? (
              <Badge variant="secondary" className="tabular">
                {m.stories.posterFormat(
                  m.values.format[d.video.format] ?? m.values.kind.video,
                  m.stories.ratio[orientation] ?? orientation
                )}
                {measuredLength(measured) ? ` · ${measuredLength(measured)}` : null}
              </Badge>
            ) : null}
            <Badge variant="outline" className="uppercase">
              {story.language}
            </Badge>
          </div>
          <Title
            lang={story.language}
            className="mt-1.5 line-clamp-2 text-lg leading-snug font-semibold max-sm:text-base"
          >
            {story.title}
          </Title>
          <p className="line-clamp-2 text-sm text-muted-foreground">
            <span
              className={cn(
                "mr-1.5 inline-block size-2 rounded-[2px] align-[0.05em]",
                hue.bg
              )}
              aria-hidden="true"
            />
            {m.stories.origin[story.origin] ?? story.origin}
            {pipelineName ? ` · ${pipelineName}` : null}
          </p>
          {story.keywords.length > 0 ? (
            <p
              className="truncate text-sm text-muted-foreground"
              aria-label={m.story.keywords}
            >
              <span lang={story.language}>{story.keywords.join(" · ")}</span>
            </p>
          ) : null}
        </Caption>
        {controls && playback && videoRef ? (
          <PlayerBar
            store={playback}
            videoRef={videoRef}
            markers={markers.map((k) => ({
              at: k.at,
              label: !k.unsent
                ? m.feed.marker(formatTimestamp(k.at), k.revision, k.scope)
                : k.text
                  ? m.feed.unsentMarker(formatTimestamp(k.at), k.scope)
                  : m.panel.yourNote(formatTimestamp(k.at)),
              unsent: k.unsent,
            }))}
            className="mt-2"
          >
            {timed ? <FeedbackTimes markers={sent} onSeek={onSeek} /> : null}
          </PlayerBar>
        ) : timed ? (
          <p className="flex justify-end">
            <FeedbackTimes markers={sent} onSeek={onSeek} />
          </p>
        ) : null}
      </div>
    </div>
  )
}

/**
 * The caption of a 9:16 draft. While it plays, the caption steps aside
 * (for screen readers it stays) so only the scrubber covers the video.
 */
function Caption({
  playback,
  children,
}: {
  playback?: PlaybackStore
  children: ReactNode
}) {
  if (!playback) return <div className="grid gap-1">{children}</div>
  return <PlayingCaption playback={playback}>{children}</PlayingCaption>
}

function PlayingCaption({
  playback,
  children,
}: {
  playback: PlaybackStore
  children: ReactNode
}) {
  const playing = usePlayback(playback, (p) => !p.paused)
  return (
    <div className={cn("grid gap-1", playing && "sr-only")}>{children}</div>
  )
}

/** "Feedback at 0:21, 0:41": each time plays the video from there. */
function FeedbackTimes({
  markers,
  onSeek,
}: {
  markers: FeedMarker[]
  onSeek: (seconds: number) => void
}) {
  return (
    <span className="ml-auto flex flex-wrap items-center justify-end gap-x-0.5 text-xs text-muted-foreground">
      <span className="mr-0.5">{m.feed.feedbackAt}</span>
      {markers.map((k, i) => (
        <span key={`${k.revision}-${k.at}-${i}`}>
          <button
            type="button"
            className="tabular rounded-sm px-0.5 font-mono text-foreground outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring"
            title={`r${k.revision} · ${k.scope}: ${k.text}`}
            aria-label={m.feed.marker(
              formatTimestamp(k.at),
              k.revision,
              k.scope
            )}
            onClick={() => onSeek(k.at)}
          >
            {formatTimestamp(k.at)}
          </button>
          {i < markers.length - 1 ? "," : null}
        </span>
      ))}
    </span>
  )
}

/** A draft next to (or far from) the current one, at its latest revision. */
export function FeedPreview({
  workspaceId,
  story,
  deliverable: d,
  near,
  pipelineName,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable | undefined
  near: boolean
  pipelineName?: string
}) {
  if (!d)
    return (
      <div className="feed-stage" inert>
        <DraftFrame
          workspaceId={workspaceId}
          story={story}
          deliverable={undefined}
          revision={undefined}
          loading={false}
          current={false}
          player={false}
          pipelineName={pipelineName}
        />
        <div className="rail max-sm:hidden" />
      </div>
    )
  return (
    <FeedPreviewOf
      workspaceId={workspaceId}
      story={story}
      deliverable={d}
      near={near}
      pipelineName={pipelineName}
    />
  )
}

function FeedPreviewOf({
  workspaceId,
  story,
  deliverable: d,
  near,
  pipelineName,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  near: boolean
  pipelineName?: string
}) {
  const revisions = useQuery({
    ...revisionsQuery(workspaceId, story.id, d),
    enabled: near && d.revision > 0,
  })
  const revision = revisions.data?.at(-1)
  return (
    <div className="feed-stage" inert>
      <DraftFrame
        workspaceId={workspaceId}
        story={story}
        deliverable={d}
        revision={near ? revision : undefined}
        loading={near && revisions.isLoading}
        current={false}
        player={near}
        pipelineName={pipelineName}
      />
      <div className="rail max-sm:hidden" />
    </div>
  )
}

/** The revision's poster frame; on a missing or broken image, the title instead. */
function PosterImage({
  src,
  title,
  lang,
}: {
  src: string
  title: string
  lang: string
}) {
  const [broken, setBroken] = useState(false)
  if (broken)
    return (
      <p
        lang={lang}
        className="absolute inset-x-[9%] top-[24%] line-clamp-5 text-2xl leading-[1.1] font-semibold tracking-tight text-balance text-white"
        aria-hidden="true"
      >
        {title}
      </p>
    )
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      decoding="async"
      onError={() => setBroken(true)}
      className="size-full object-contain"
    />
  )
}

function FrameLoading() {
  return (
    <div className="grid gap-3" aria-busy="true" aria-label={m.common.loading}>
      <Skeleton className="h-8 w-3/4" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-4 w-5/6" />
    </div>
  )
}

function NothingYet({ deliverable: d }: { deliverable: Deliverable }) {
  return (
    <p className="flex items-center gap-2 text-sm text-muted-foreground">
      {d.status === "drafting" || d.status === "queued" ? (
        <Loader2Icon className="size-4 animate-spin" />
      ) : null}
      {m.preview.nothingYet}
    </p>
  )
}
