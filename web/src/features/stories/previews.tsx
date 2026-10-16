import { useMemo, useSyncExternalStore, type ReactNode, type RefObject } from "react"
import { useQuery } from "@tanstack/react-query"
import {
  DownloadIcon,
  MessageSquareDashedIcon,
  MessageSquareIcon,
} from "lucide-react"

import type { Revision } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { artifactTextQuery, artifactUrl } from "@/queries/stories"
import { Markdown, fmList, fmText, splitFrontmatter } from "./markdown"
import { formatTimestamp, parseTimestamp } from "./model"
import type { PlaybackStore } from "./player"

export type ArtifactRef = {
  workspaceId: string
  storyId: string
  deliverableId: string
  revision: number
}

const url = (a: ArtifactRef, name: string, download = false) =>
  artifactUrl(
    a.workspaceId,
    a.storyId,
    a.deliverableId,
    a.revision,
    name,
    download
  )

/** The text of an artifact of the revision, when it has one. */
function useArtifactText(a: ArtifactRef, name: string, present: boolean) {
  return useQuery({
    ...artifactTextQuery(
      a.workspaceId,
      a.storyId,
      a.deliverableId,
      a.revision,
      name
    ),
    enabled: present,
  })
}

/** A titled block of the preview column. */
function Block({
  title,
  children,
  className,
}: {
  title: string
  children: ReactNode
  className?: string
}) {
  return (
    <section className={cn("grid gap-3", className)}>
      <h3 className="text-sm font-medium">
        {title}
      </h3>
      {children}
    </section>
  )
}

function TextState({
  query,
}: {
  query: { isPending: boolean; isError: boolean; error: unknown }
}) {
  if (query.isPending) {
    return (
      <div
        className="grid gap-2"
        aria-busy="true"
        aria-label={m.common.loading}
      >
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
      </div>
    )
  }
  if (query.isError) {
    return (
      <p className="text-sm text-destructive">
        {m.preview.loadFailed} {errorMessage(query.error)}
      </p>
    )
  }
  return null
}

// ---- Video ----

/** The revision's video file: the final render once there is one, else the preview. */
export function videoFile(revision: Revision): string | undefined {
  const has = (name: string) => revision.artifacts.includes(name)
  return has("final.mp4")
    ? "final.mp4"
    : has("preview.mp4")
      ? "preview.mp4"
      : undefined
}

/**
 * The <video> of a revision's render, with its poster frame. With
 * `playback` the review's own controls drive it (a click plays or
 * pauses); without, it is a still preview.
 */
export function VideoFrameElement({
  artifacts: a,
  revision,
  file,
  videoRef,
  playback,
  className,
}: {
  artifacts: ArtifactRef
  revision: Revision
  file: string
  videoRef?: RefObject<HTMLVideoElement | null>
  playback?: PlaybackStore
  className?: string
}) {
  const poster = revision.artifacts.includes("poster.jpg")
    ? url(a, "poster.jpg")
    : undefined
  return (
    // No caption track is produced; the script is the transcript. The
    // click is a shortcut for the play button beside the scrubber.
    // oxlint-disable-next-line jsx-a11y/media-has-caption, jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions
    <video
      // A new source (another revision, or the final render) starts afresh.
      key={url(a, file)}
      ref={videoRef}
      playsInline
      preload="metadata"
      poster={poster}
      className={cn("block bg-black", playback && "cursor-pointer", className)}
      {...playback?.events}
      onClick={
        playback
          ? (e) => {
              const el = e.currentTarget
              if (el.paused) void el.play().catch(() => undefined)
              else el.pause()
            }
          : undefined
      }
    >
      <source src={url(a, file)} type="video/mp4" />
    </video>
  )
}

/** Plays the video from a position (chapters, feedback markers). */
export function seekVideo(
  videoRef: RefObject<HTMLVideoElement | null>,
  seconds: number
) {
  const el = videoRef.current
  if (!el) return
  el.currentTime = seconds
  void el.play().catch(() => undefined)
}

/** The revision's metadata (chapters seek the player) and narration. */
export function VideoDetails({
  artifacts: a,
  revision,
  videoRef,
  lang,
}: {
  artifacts: ArtifactRef
  revision: Revision
  videoRef: RefObject<HTMLVideoElement | null>
  /** The video's language (the metadata is written in it). */
  lang: string
}) {
  const has = (name: string) => revision.artifacts.includes(name)
  const video = videoFile(revision)
  const metadata = useArtifactText(a, "metadata.json", has("metadata.json"))
  const seek = (seconds: number) => seekVideo(videoRef, seconds)

  return (
    <>
      {has("metadata.json") ? (
        <Block title={m.preview.metadata}>
          {metadata.data !== undefined ? (
            <MetadataView
              text={metadata.data}
              lang={lang}
              onSeek={video ? seek : undefined}
            />
          ) : (
            <TextState query={metadata} />
          )}
        </Block>
      ) : null}

      {has("narration.wav") ? (
        <Block title={m.preview.narration}>
          {/* The script is the narration's transcript. */}
          {/* oxlint-disable-next-line jsx-a11y/media-has-caption */}
          <audio
            controls
            preload="none"
            src={url(a, "narration.wav")}
            className="w-full"
          />
        </Block>
      ) : null}
    </>
  )
}

/**
 * A note pinned to a time, shown on the script's scene that holds it:
 * `label` says whose and when, `text` what it asks. `unsent` marks the
 * reviewer's own notes that have not gone to the agent yet.
 */
export type ScriptNote = {
  at: number
  label: string
  text?: string
  unsent?: boolean
}

/**
 * The revision's script (the narration); null when it has none. A script
 * whose scene headings carry their start ("## Sahne 2 · 0:09") reads as
 * timed rows: a note's scene is marked, the playhead's scene is tinted,
 * and a time plays the video from there. Any other script is rendered
 * as it is; no times are made up.
 */
export function ScriptText({
  artifacts: a,
  revision,
  lang,
  notes = [],
  playback,
  onSeek,
}: {
  artifacts: ArtifactRef
  revision: Revision
  /** The script's language. */
  lang: string
  notes?: ScriptNote[]
  playback?: PlaybackStore
  onSeek?: (seconds: number) => void
}) {
  const present = revision.artifacts.includes("script.md")
  const script = useArtifactText(a, "script.md", present)
  const parsed = useMemo(
    () => (script.data !== undefined ? parseScenes(script.data) : undefined),
    [script.data]
  )
  if (!present) return null
  if (script.data === undefined) return <TextState query={script} />
  if (!parsed)
    return (
      <Markdown
        source={script.data}
        lang={lang}
        className="markdown-compact text-sm"
      />
    )
  return (
    <SceneList
      lang={lang}
      intro={parsed.intro}
      scenes={parsed.scenes}
      notes={notes}
      playback={playback}
      onSeek={onSeek}
    />
  )
}

type Scene = { at: number; label: string; body: string }

const SCENE_HEADING =
  /^#{1,6}\s+(.*?)\s*(?:[·•|–—-]\s*)?\(?(\d{1,2}:\d{2}(?::\d{2})?)\)?\s*$/

/**
 * Splits a script at headings that end in a start time. Undefined unless
 * every scene has one and they run forward, so the rows never guess.
 */
export function parseScenes(
  text: string
): { intro: string; scenes: Scene[] } | undefined {
  const intro: string[] = []
  const scenes: Scene[] = []
  let body: string[] | null = null
  for (const line of text.split(/\r?\n/)) {
    const h = SCENE_HEADING.exec(line)
    const at = h ? parseTimestamp(h[2]) : undefined
    if (h && at !== undefined) {
      body = []
      scenes.push({ at, label: h[1].trim(), body: "" })
      continue
    }
    // Another heading after the scenes began: not a timed script.
    if (body && /^#{1,6}\s/.test(line)) return undefined
    ;(body ?? intro).push(line)
    if (body) scenes[scenes.length - 1].body = body.join("\n").trim()
  }
  if (scenes.length === 0) return undefined
  if (scenes.some((sc, i) => i > 0 && sc.at <= scenes[i - 1].at))
    return undefined
  // The script's own title repeats the story's.
  const lead = intro.join("\n").replace(/^\s*#\s+.*$/m, "").trim()
  return { intro: lead, scenes }
}

/** The scene that holds `t`: the last one starting at or before it. */
function sceneAt(scenes: Scene[], t: number): number {
  let found = -1
  for (let i = 0; i < scenes.length; i++) if (scenes[i].at <= t) found = i
  return found
}

function SceneList({
  lang,
  intro,
  scenes,
  notes,
  playback,
  onSeek,
}: {
  lang: string
  intro: string
  scenes: Scene[]
  notes: ScriptNote[]
  playback?: PlaybackStore
  onSeek?: (seconds: number) => void
}) {
  const active = useActiveScene(scenes, playback)
  const noted = new Map<number, ScriptNote[]>()
  for (const n of notes) {
    const i = sceneAt(scenes, n.at)
    if (i >= 0) noted.set(i, [...(noted.get(i) ?? []), n])
  }
  return (
    <div className="grid gap-3">
      {intro ? (
        <Markdown
          source={intro}
          lang={lang}
          className="markdown-compact text-sm"
        />
      ) : null}
      <ol className="-mx-2 grid gap-1">
        {scenes.map((sc, i) => {
          const marks = noted.get(i)
          const time = formatTimestamp(sc.at)
          return (
            <li
              key={`${sc.at}-${i}`}
              aria-current={i === active ? "time" : undefined}
              className={cn(
                "grid grid-cols-[2.75rem_minmax(0,1fr)] gap-x-2 rounded-lg px-2 py-2 transition-colors",
                marks ? "bg-muted" : i === active && "bg-muted/50"
              )}
            >
              {onSeek ? (
                <button
                  type="button"
                  className={cn(
                    "tabular h-fit justify-self-start rounded-sm font-mono text-xs leading-5 outline-none hover:text-foreground hover:underline focus-visible:ring-[3px] focus-visible:ring-ring",
                    i === active ? "text-foreground" : "text-muted-foreground"
                  )}
                  aria-label={m.preview.seek(time)}
                  onClick={() => onSeek(sc.at)}
                >
                  {time}
                </button>
              ) : (
                <span className="tabular font-mono text-xs leading-5 text-muted-foreground">
                  {time}
                </span>
              )}
              <div className="min-w-0">
                {sc.label ? (
                  <span lang={lang} className="sr-only">
                    {sc.label}
                  </span>
                ) : null}
                <Markdown
                  source={sc.body}
                  lang={lang}
                  className="markdown-compact text-sm leading-relaxed"
                />
                {marks?.map((n, j) => {
                  const Icon = n.unsent ? MessageSquareDashedIcon : MessageSquareIcon
                  return (
                    <div key={`${n.at}-${j}`} className="mt-1.5 grid gap-0.5 text-xs">
                      <p className="flex items-center gap-1.5 font-medium">
                        <Icon
                          className="size-3.5 shrink-0 text-muted-foreground"
                          aria-hidden="true"
                        />
                        {n.label}
                      </p>
                      {n.text ? (
                        <p
                          lang={lang}
                          className="line-clamp-2 pl-5 text-muted-foreground"
                        >
                          “{n.text}”
                        </p>
                      ) : null}
                    </div>
                  )
                })}
              </div>
            </li>
          )
        })}
      </ol>
    </div>
  )
}

/** The scene under the playhead, once the video has been played or moved. */
function useActiveScene(scenes: Scene[], playback?: PlaybackStore): number {
  return useSyncExternalStore(
    playback?.subscribe ?? noSubscribe,
    () => {
      const p = playback?.get()
      if (!p || (p.paused && p.time === 0)) return -1
      return sceneAt(scenes, p.time)
    }
  )
}

const noSubscribe = () => () => undefined

type Metadata = {
  title?: string
  description?: string
  tags: string[]
  chapters: Array<{ at: number; title: string }>
}

const str = (v: unknown) => (typeof v === "string" && v.trim() ? v : undefined)

function parseMetadata(text: string): Metadata | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(text)
  } catch {
    return undefined
  }
  if (typeof raw !== "object" || raw === null) return undefined
  const r = raw as Record<string, unknown>
  const chapters: Metadata["chapters"] = []
  if (Array.isArray(r.chapters)) {
    for (const c of r.chapters) {
      if (typeof c !== "object" || c === null) continue
      const ch = c as Record<string, unknown>
      const at = parseTimestamp(ch.at)
      const title = str(ch.title)
      if (at !== undefined && title) chapters.push({ at, title })
    }
  }
  return {
    title: str(r.title),
    description: str(r.description),
    tags: Array.isArray(r.tags)
      ? r.tags.filter((t): t is string => typeof t === "string")
      : [],
    chapters,
  }
}

function MetadataView({
  text,
  lang,
  onSeek,
}: {
  text: string
  lang: string
  onSeek?: (seconds: number) => void
}) {
  const meta = useMemo(() => parseMetadata(text), [text])
  if (!meta) {
    return (
      <pre
        lang={lang}
        className="overflow-x-auto rounded-lg bg-muted p-3 font-mono text-xs whitespace-pre-wrap"
      >
        {text}
      </pre>
    )
  }
  return (
    <div className="grid gap-4 rounded-xl border p-4 md:p-5">
      {meta.title ? (
        <p
          lang={lang}
          className="font-heading text-lg leading-snug font-semibold"
        >
          {meta.title}
        </p>
      ) : null}
      {meta.description ? (
        <p
          lang={lang}
          className="text-sm whitespace-pre-wrap text-muted-foreground"
        >
          {meta.description}
        </p>
      ) : null}
      {meta.tags.length > 0 ? (
        <Tags tags={meta.tags} label={m.preview.tags} lang={lang} />
      ) : null}
      {meta.chapters.length > 0 ? (
        <div className="grid gap-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            {m.preview.chapters}
          </p>
          <ol className="grid gap-0.5">
            {meta.chapters.map((c) => (
              <li key={`${c.at}-${c.title}`}>
                {onSeek ? (
                  <button
                    type="button"
                    className="-mx-2 flex w-full items-baseline gap-3 rounded-xl px-2 py-1 text-left text-sm hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:outline-none"
                    onClick={() => onSeek(c.at)}
                    title={m.preview.seek(formatTimestamp(c.at))}
                  >
                    <span className="tabular w-12 shrink-0 font-mono text-xs text-muted-foreground">
                      {formatTimestamp(c.at)}
                    </span>
                    <span lang={lang}>{c.title}</span>
                  </button>
                ) : (
                  <span className="flex items-baseline gap-3 py-1 text-sm">
                    <span className="tabular w-12 shrink-0 font-mono text-xs text-muted-foreground">
                      {formatTimestamp(c.at)}
                    </span>
                    <span lang={lang}>{c.title}</span>
                  </span>
                )}
              </li>
            ))}
          </ol>
        </div>
      ) : null}
    </div>
  )
}

function Tags({
  tags,
  label,
  lang,
}: {
  tags: string[]
  label: string
  lang: string
}) {
  return (
    <ul className="flex flex-wrap gap-1.5" aria-label={label}>
      {tags.map((t) => (
        <li key={t}>
          <Badge variant="outline" className="font-normal" lang={lang}>
            {t}
          </Badge>
        </li>
      ))}
    </ul>
  )
}

// ---- Article ----

/** The revision's article.md: frontmatter as a header, the body rendered. */
export function ArticlePreview({
  artifacts: a,
  revision,
  lang,
}: {
  artifacts: ArtifactRef
  revision: Revision
  /** The article's language. */
  lang: string
}) {
  const present = revision.artifacts.includes("article.md")
  const article = useArtifactText(a, "article.md", present)
  const parsed = useMemo(
    () =>
      article.data !== undefined ? splitFrontmatter(article.data) : undefined,
    [article.data]
  )

  if (!present) {
    return (
      <div className="rounded-xl border border-dashed px-6 py-10 text-center text-sm text-muted-foreground">
        {m.preview.noArticle}
      </div>
    )
  }
  if (!parsed) return <TextState query={article} />

  const { data, body } = parsed
  const title = fmText(data, "title")
  const description = fmText(data, "description")
  const seoTitle = fmText(data, "seoTitle")
  const slug = fmText(data, "slug")
  const tags = fmList(data, "tags")

  return (
    <article className="grid gap-6">
      <header className="grid gap-3 rounded-xl border bg-muted/20 p-5 md:p-6">
        {title ? (
          <h2
            lang={lang}
            className="font-heading text-2xl leading-tight font-semibold tracking-tight"
          >
            {title}
          </h2>
        ) : null}
        {description ? (
          <p lang={lang} className="text-muted-foreground">
            {description}
          </p>
        ) : null}
        <dl className="grid gap-x-6 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
          {slug ? (
            <>
              <dt className="text-muted-foreground">{m.preview.slug}</dt>
              <dd className="font-mono text-[13px] break-all">
                /{slug.replace(/^\//, "")}
              </dd>
            </>
          ) : null}
          {seoTitle ? (
            <>
              <dt className="text-muted-foreground">{m.preview.seoTitle}</dt>
              <dd lang={lang}>{seoTitle}</dd>
            </>
          ) : null}
          {tags.length > 0 ? (
            <>
              <dt className="text-muted-foreground">{m.preview.tags}</dt>
              <dd>
                <Tags tags={tags} label={m.preview.tags} lang={lang} />
              </dd>
            </>
          ) : null}
        </dl>
      </header>
      <Markdown source={body} lang={lang} className="max-w-[72ch] text-[15px]" />
    </article>
  )
}

// ---- Files ----

/** Every downloadable artifact of the revision. */
export function FilesList({
  artifacts: a,
  revision,
}: {
  artifacts: ArtifactRef
  revision: Revision
}) {
  // The agent's packed working directory is internal and not served.
  const names = revision.artifacts.filter((n) => !n.endsWith(".tar.gz"))
  if (names.length === 0) return null
  return (
    <Block title={m.preview.files}>
      <ul className="flex flex-wrap gap-1.5">
        {names.map((name) => (
          <li key={name}>
            <a
              href={url(a, name, true)}
              download={name}
              className="inline-flex h-7 items-center gap-1.5 rounded-full border px-3 font-mono text-xs outline-none hover:bg-muted focus-visible:ring-[3px] focus-visible:ring-ring"
              aria-label={m.preview.download(name)}
            >
              <DownloadIcon className="size-3.5 text-muted-foreground" />
              {name}
            </a>
          </li>
        ))}
      </ul>
    </Block>
  )
}
