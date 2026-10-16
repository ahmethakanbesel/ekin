import {
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
  type SyntheticEvent,
} from "react"
import {
  MaximizeIcon,
  MinimizeIcon,
  PauseIcon,
  PlayIcon,
  Volume2Icon,
  VolumeXIcon,
} from "lucide-react"

import { Button } from "@/components/ui/button"
import { m } from "@/i18n"
import { cn } from "@/lib/utils"
import { formatTimestamp } from "./model"

export type Playback = {
  time: number
  /** Unknown until the video's metadata has loaded. */
  duration?: number
  paused: boolean
  muted: boolean
}

type MediaEvent = SyntheticEvent<HTMLVideoElement>

const known = (n: number) => (Number.isFinite(n) && n > 0 ? n : undefined)

/**
 * Where the review's player is, fed by the <video>'s own events. Parts
 * that show it subscribe to the slice they need, so the page does not
 * re-render as the video plays.
 */
export function createPlayback() {
  let state: Playback = { time: 0, paused: true, muted: false }
  const subscribers = new Set<() => void>()
  const set = (next: Partial<Playback>) => {
    state = { ...state, ...next }
    for (const f of subscribers) f()
  }
  const timing = (e: MediaEvent) =>
    set({
      time: e.currentTarget.currentTime,
      duration: known(e.currentTarget.duration),
    })
  return {
    get: () => state,
    set,
    subscribe: (f: () => void) => {
      subscribers.add(f)
      return () => {
        subscribers.delete(f)
      }
    },
    /** Spread on the <video>. */
    events: {
      onLoadStart: () => set({ time: 0, duration: undefined, paused: true }),
      onLoadedMetadata: (e: MediaEvent) => {
        timing(e)
        set({ muted: e.currentTarget.muted })
      },
      onDurationChange: timing,
      onTimeUpdate: timing,
      onSeeked: timing,
      onPlay: () => set({ paused: false }),
      onPause: () => set({ paused: true }),
      onEnded: () => set({ paused: true }),
      onVolumeChange: (e: MediaEvent) => set({ muted: e.currentTarget.muted }),
    },
  }
}

export type PlaybackStore = ReturnType<typeof createPlayback>

/** A slice of the player's state; re-renders only when it changes. */
export function usePlayback<T>(
  store: PlaybackStore,
  select: (p: Playback) => T
): T {
  return useSyncExternalStore(store.subscribe, () => select(store.get()))
}

export type PlayerMarker = { at: number; label: string }

const STEP = 5
const PAGE = 10

/**
 * The scrubber under a draft: a track with the progress in marigold and
 * a dot at every note pinned to a time (a click there plays from it),
 * then play / pause and the time.
 */
export function PlayerBar({
  store,
  videoRef,
  markers,
  className,
  children,
}: {
  store: PlaybackStore
  videoRef: RefObject<HTMLVideoElement | null>
  markers: PlayerMarker[]
  className?: string
  /** After the time (the feedback times). */
  children?: ReactNode
}) {
  const time = usePlayback(store, (p) => p.time)
  const duration = usePlayback(store, (p) => p.duration)
  const paused = usePlayback(store, (p) => p.paused)
  const track = useRef<HTMLDivElement>(null)
  const thumb = useRef<HTMLSpanElement>(null)
  const max = duration ?? 0
  const pct = (t: number) =>
    max > 0 ? `${Math.min(100, Math.max(0, (t / max) * 100))}%` : "0%"

  const seek = (t: number) => {
    const el = videoRef.current
    if (!el || !max) return
    const at = Math.min(max, Math.max(0, t))
    el.currentTime = at
    store.set({ time: at })
  }
  const toggle = () => {
    const el = videoRef.current
    if (!el) return
    if (el.paused) void el.play().catch(() => undefined)
    else el.pause()
  }
  const fromPointer = (e: PointerEvent) => {
    const r = track.current?.getBoundingClientRect()
    return r && r.width > 0 ? ((e.clientX - r.left) / r.width) * max : 0
  }

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (!max || e.button !== 0) return
    e.preventDefault()
    e.currentTarget.setPointerCapture(e.pointerId)
    // A note's dot plays from the note's own time.
    const dot = (e.target as HTMLElement).closest<HTMLElement>("[data-at]")
    if (dot) {
      seek(Number(dot.dataset.at))
      void videoRef.current?.play().catch(() => undefined)
    } else seek(fromPointer(e))
    thumb.current?.focus({ preventScroll: true })
  }
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (e.currentTarget.hasPointerCapture(e.pointerId)) seek(fromPointer(e))
  }
  const onKeyDown = (e: KeyboardEvent) => {
    const by: Record<string, number> = {
      ArrowLeft: -STEP,
      ArrowDown: -STEP,
      ArrowRight: STEP,
      ArrowUp: STEP,
      PageDown: -PAGE,
      PageUp: PAGE,
    }
    if (e.key in by) seek(store.get().time + by[e.key])
    else if (e.key === "Home") seek(0)
    else if (e.key === "End") seek(max)
    else if (e.key === " " || e.key === "Enter") toggle()
    else return
    e.preventDefault()
  }

  const now = formatTimestamp(time)
  const total = duration !== undefined ? formatTimestamp(duration) : undefined
  return (
    <div className={cn("grid gap-1", className)}>
      <div
        ref={track}
        className={cn(
          "relative flex h-5 w-full touch-none items-center select-none",
          max > 0 ? "cursor-pointer" : "opacity-60"
        )}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
      >
        <span className="relative h-1.5 w-full overflow-hidden rounded-full bg-muted inset-ring inset-ring-input">
          <span
            className="absolute inset-y-0 left-0 bg-primary inset-ring inset-ring-primary-ink"
            style={{ width: pct(time) }}
          />
        </span>
        {max > 0
          ? markers.map((k) => (
              <span
                key={`${k.at}-${k.label}`}
                data-at={k.at}
                title={k.label}
                aria-hidden="true"
                className="absolute -top-1 z-10 grid size-4 -translate-x-1/2 place-items-center"
                style={{ left: pct(k.at) }}
              >
                <span className="size-2 rounded-full border border-foreground bg-background" />
              </span>
            ))
          : null}
        <span
          ref={thumb}
          role="slider"
          tabIndex={0}
          aria-label={m.player.position}
          aria-valuemin={0}
          aria-valuemax={Math.round(max)}
          aria-valuenow={Math.round(time)}
          aria-valuetext={total ? m.player.valueText(now, total) : now}
          aria-disabled={max > 0 ? undefined : true}
          className="absolute z-20 size-4 -translate-x-1/2 rounded-full border border-primary bg-white shadow-sm ring-ring/50 transition-shadow outline-none hover:ring-4 focus-visible:ring-4 focus-visible:ring-ring"
          style={{ left: pct(time) }}
          onKeyDown={onKeyDown}
        />
      </div>
      <div className="flex items-center gap-2">
        <Button
          variant="ghost"
          size="icon-sm"
          className="-ml-1.5"
          aria-label={paused ? m.player.play : m.player.pause}
          onClick={toggle}
        >
          {paused ? <PlayIcon /> : <PauseIcon />}
        </Button>
        <span className="tabular font-mono text-xs">
          {now}
          {total ? (
            <span className="text-muted-foreground"> / {total}</span>
          ) : null}
        </span>
        {children}
      </div>
    </div>
  )
}

/** Mute and full screen, over the top right of the video. */
export function PlayerCorner({
  store,
  videoRef,
  className,
}: {
  store: PlaybackStore
  videoRef: RefObject<HTMLVideoElement | null>
  className?: string
}) {
  const muted = usePlayback(store, (p) => p.muted)
  const [full, setFull] = useState(false)
  useEffect(() => {
    const on = () => setFull(document.fullscreenElement !== null)
    document.addEventListener("fullscreenchange", on)
    return () => document.removeEventListener("fullscreenchange", on)
  }, [])

  const fullscreen = () => {
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => undefined)
      return
    }
    const el = videoRef.current as
      | (HTMLVideoElement & { webkitEnterFullscreen?: () => void })
      | null
    // The whole frame, so the scrubber comes along; iPhones only allow the video.
    const frame = el?.closest<HTMLElement>("[data-player]")
    if (frame && document.fullscreenEnabled)
      void frame.requestFullscreen().catch(() => undefined)
    else el?.webkitEnterFullscreen?.()
  }

  return (
    <div className={cn("flex items-center gap-1.5", className)}>
      <Button
        variant="secondary"
        size="icon-sm"
        aria-label={muted ? m.player.unmute : m.player.mute}
        onClick={() => {
          const el = videoRef.current
          if (el) el.muted = !el.muted
        }}
      >
        {muted ? <VolumeXIcon /> : <Volume2Icon />}
      </Button>
      <Button
        variant="secondary"
        size="icon-sm"
        aria-label={full ? m.player.exitFullscreen : m.player.fullscreen}
        onClick={fullscreen}
      >
        {full ? <MinimizeIcon /> : <MaximizeIcon />}
      </Button>
    </div>
  )
}
