import { useEffect, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  Loader2Icon,
  PlusIcon,
  SparklesIcon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"

import type {
  Deliverable,
  Moment,
  Moments,
  Sentence,
  Story,
} from "@/api/client"
import { ErrorState } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { badgeTone } from "@/lib/tones"
import { cn } from "@/lib/utils"
import { keys } from "@/queries/keys"
import {
  momentsQuery,
  useAddMoment,
  useAdjustMoment,
  useDiscardMoment,
  useMakeShorts,
  useScanMoments,
} from "@/queries/stories"
import { jobQuery } from "@/queries/templates"
import { formatTimestamp } from "./model"

const SCAN_POLL_MS = 2000

/** "0:51–1:11" */
export function clipRange(c: {
  startSeconds: number
  endSeconds: number
}): string {
  return `${formatTimestamp(c.startSeconds)}–${formatTimestamp(c.endSeconds)}`
}

/**
 * The moments of a live long video that could stand alone as shorts:
 * the agent's suggestions and a person's own, which become shorts of
 * the same story. Shown in the review panel beside the long video.
 */
export function ShortsPanel({
  workspaceId,
  story,
  deliverable: d,
  canEdit,
  onSeek,
  onPick,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  canEdit: boolean
  /** Plays the long video from a position, in seconds. */
  onSeek?: (seconds: number) => void
  /** Shows another deliverable of the story (a short made from a moment). */
  onPick: (deliverableId: string) => void
}) {
  const shortsMade = story.deliverables.filter(
    (x) => x.sourceId === d.id && x.clip
  ).length
  const moments = useQuery(
    momentsQuery(workspaceId, story.id, d.id, shortsMade)
  )

  if (moments.isPending) {
    return <p className="text-sm text-muted-foreground">{m.shorts.loading}</p>
  }
  if (moments.isError) {
    return (
      <ErrorState
        error={moments.error}
        onRetry={() => void moments.refetch()}
      />
    )
  }
  const data = moments.data
  if (data.blocked) {
    return (
      <p className="text-sm text-muted-foreground">{sentence(data.blocked)}</p>
    )
  }
  return (
    <MomentsBody
      workspaceId={workspaceId}
      story={story}
      deliverable={d}
      data={data}
      canEdit={canEdit}
      onSeek={onSeek}
      onPick={onPick}
    />
  )
}

function MomentsBody({
  workspaceId,
  story,
  deliverable: d,
  data,
  canEdit,
  onSeek,
  onPick,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  data: Moments
  canEdit: boolean
  onSeek?: (seconds: number) => void
  onPick: (deliverableId: string) => void
}) {
  const [chosen, setChosen] = useState<ReadonlySet<string>>(new Set())
  const make = useMakeShorts(workspaceId, story.id, d.id)
  const room = Math.max(0, data.limits.maxPerVideo - data.shorts)
  const open = data.moments.filter((x) => x.status === "suggested")
  const picked = open.filter((x) => chosen.has(x.id))

  const toggle = (id: string, on: boolean) =>
    setChosen((prev) => {
      const next = new Set(prev)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })

  const makeShorts = () =>
    make.mutate(
      picked.map((x) => x.id),
      {
        onSuccess: () => {
          setChosen(new Set())
          toast.success(m.shorts.made(picked.length))
        },
        onError: (err) =>
          toast.error(m.shorts.makeFailed, { description: errorMessage(err) }),
      }
    )

  return (
    <div className="grid gap-4">
      <p className="text-sm text-muted-foreground">
        {m.shorts.summary(
          data.shorts,
          data.limits.maxPerVideo,
          data.limits.clipMinSeconds,
          data.limits.clipMaxSeconds
        )}
      </p>
      {canEdit ? (
        <ScanButton workspaceId={workspaceId} story={story} deliverable={d} />
      ) : null}
      {data.moments.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {canEdit ? m.shorts.emptyEditor : m.shorts.empty}
        </p>
      ) : (
        <ul
          className="divide-y rounded-xl border"
          aria-label={m.shorts.listLabel}
        >
          {data.moments.map((x) => (
            <MomentRow
              key={x.id}
              workspaceId={workspaceId}
              story={story}
              deliverable={d}
              moment={x}
              sentences={data.sentences}
              canEdit={canEdit}
              chosen={chosen.has(x.id)}
              onChoose={(on) => toggle(x.id, on)}
              onSeek={onSeek}
              onPick={onPick}
            />
          ))}
        </ul>
      )}
      {canEdit && open.length > 0 ? (
        <div className="flex flex-wrap items-center gap-3">
          <Button
            size="sm"
            variant={picked.length > 0 ? "default" : "outline"}
            disabled={
              picked.length === 0 || picked.length > room || make.isPending
            }
            onClick={makeShorts}
          >
            {make.isPending ? (
              <Loader2Icon data-icon="inline-start" className="animate-spin" />
            ) : null}
            {m.shorts.make(picked.length)}
          </Button>
          <span className="text-xs text-muted-foreground">
            {picked.length > room
              ? m.shorts.overRoom(room)
              : m.shorts.room(room)}
          </span>
        </div>
      ) : null}
      {canEdit && room > 0 ? (
        <SentencePicker
          workspaceId={workspaceId}
          story={story}
          deliverable={d}
          sentences={data.sentences}
          limits={data.limits}
          onSeek={onSeek}
        />
      ) : null}
    </div>
  )
}

/** Asks the agent for moments and follows its job until it ends. */
function ScanButton({
  workspaceId,
  story,
  deliverable: d,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
}) {
  const qc = useQueryClient()
  const scan = useScanMoments(workspaceId, story.id, d.id)
  const [jobId, setJobId] = useState<string>()
  const job = useQuery({
    ...jobQuery(workspaceId, jobId ?? ""),
    enabled: !!jobId,
    refetchInterval: (q) =>
      ended(q.state.data?.status) ? false : SCAN_POLL_MS,
  })
  const status = job.data?.status
  const lastError = job.data?.lastError

  // The scan's outcome: fresh moments, or why there are none.
  useEffect(() => {
    if (status === "succeeded") {
      void qc.invalidateQueries({
        queryKey: keys.moments(workspaceId, story.id, d.id),
      })
    } else if (status === "failed" || status === "cancelled") {
      toast.error(m.shorts.findFailed, { description: lastError || undefined })
    }
  }, [status, lastError, qc, workspaceId, story.id, d.id])

  const busy = scan.isPending || (!!jobId && !ended(status))
  return (
    <div className="flex flex-wrap items-center gap-3">
      <Button
        size="sm"
        variant="outline"
        disabled={busy}
        onClick={() =>
          scan.mutate(undefined, {
            onSuccess: (j) => setJobId(j.id),
            onError: (err) =>
              toast.error(m.shorts.findFailed, {
                description: errorMessage(err),
              }),
          })
        }
      >
        {busy ? (
          <Loader2Icon data-icon="inline-start" className="animate-spin" />
        ) : (
          <SparklesIcon data-icon="inline-start" />
        )}
        {busy ? m.shorts.finding : m.shorts.find}
      </Button>
      <span className="text-xs text-muted-foreground">{m.shorts.findHint}</span>
    </div>
  )
}

function ended(status: string | undefined): boolean {
  return status === "succeeded" || status === "failed" || status === "cancelled"
}

/** The sentence a time starts or ends, by the closest boundary. */
function nearest(
  sentences: Sentence[],
  seconds: number,
  edge: "startSeconds" | "endSeconds"
): number {
  let best = 0
  sentences.forEach((s, i) => {
    if (Math.abs(s[edge] - seconds) < Math.abs(sentences[best][edge] - seconds))
      best = i
  })
  return best
}

function MomentRow({
  workspaceId,
  story,
  deliverable: d,
  moment: x,
  sentences,
  canEdit,
  chosen,
  onChoose,
  onSeek,
  onPick,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  moment: Moment
  sentences: Sentence[]
  canEdit: boolean
  chosen: boolean
  onChoose: (on: boolean) => void
  onSeek?: (seconds: number) => void
  onPick: (deliverableId: string) => void
}) {
  const adjust = useAdjustMoment(workspaceId, story.id, d.id)
  const discard = useDiscardMoment(workspaceId, story.id, d.id)
  const open = x.status === "suggested"
  const length = Math.round(x.clip.endSeconds - x.clip.startSeconds)
  const range = clipRange(x.clip)
  const first = nearest(sentences, x.clip.startSeconds, "startSeconds")
  const last = nearest(sentences, x.clip.endSeconds, "endSeconds")
  const checkboxId = `moment-${x.id}`
  const shortId = x.shortId

  const move = (from: number, to: number) => {
    if (from < 0 || to >= sentences.length || from > to) return
    adjust.mutate(
      {
        momentId: x.id,
        body: {
          startSeconds: sentences[from].startSeconds,
          endSeconds: sentences[to].endSeconds,
        },
      },
      {
        onError: (err) =>
          toast.error(m.shorts.adjustFailed, {
            description: errorMessage(err),
          }),
      }
    )
  }

  return (
    <li
      className={cn(
        "grid gap-1.5 p-3",
        x.status === "discarded" && "text-muted-foreground"
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        {canEdit && open ? (
          <Checkbox
            id={checkboxId}
            checked={chosen}
            onCheckedChange={(v) => onChoose(v === true)}
            aria-label={m.shorts.choose(range)}
          />
        ) : null}
        {onSeek ? (
          <button
            type="button"
            className="rounded-sm font-mono text-xs tabular-nums underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring"
            onClick={() => onSeek(x.clip.startSeconds)}
            aria-label={m.shorts.play(range)}
          >
            {range}
          </button>
        ) : (
          <span className="font-mono text-xs tabular-nums">{range}</span>
        )}
        <span className="text-xs text-muted-foreground">
          {m.shorts.seconds(length)}
        </span>
        <span className="ml-auto flex flex-wrap items-center gap-1.5">
          {x.origin === "manual" ? (
            <Badge variant="outline" className={badgeTone.neutral}>
              {m.shorts.yours}
            </Badge>
          ) : (
            <Badge variant="outline" className={badgeTone.neutral}>
              {m.shorts.score(x.score)}
            </Badge>
          )}
          {x.status === "made" ? (
            <Badge variant="outline" className={badgeTone.success}>
              {m.shorts.status.made}
            </Badge>
          ) : x.status === "discarded" ? (
            <Badge
              variant="outline"
              className={cn(badgeTone.muted, "line-through decoration-1")}
            >
              {m.shorts.status.discarded}
            </Badge>
          ) : null}
        </span>
      </div>
      {x.hook ? (
        <label
          htmlFor={canEdit && open ? checkboxId : undefined}
          className={cn(
            "text-sm font-medium",
            x.status === "discarded" && "line-through decoration-1"
          )}
        >
          {x.hook}
        </label>
      ) : null}
      {x.reason ? (
        <p className="text-xs text-muted-foreground">{x.reason}</p>
      ) : null}
      {x.status === "made" && shortId ? (
        <div>
          <Button
            size="xs"
            variant="ghost"
            className="-ml-2"
            onClick={() => onPick(shortId)}
          >
            {m.shorts.openShort}
            <ChevronRightIcon data-icon="inline-end" />
          </Button>
        </div>
      ) : null}
      {canEdit && open ? (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span className="flex items-center gap-0.5">
            {m.shorts.start}
            <Button
              size="icon-xs"
              variant="ghost"
              disabled={adjust.isPending || first === 0}
              onClick={() => move(first - 1, last)}
              aria-label={m.shorts.startEarlier}
            >
              <ChevronLeftIcon />
            </Button>
            <Button
              size="icon-xs"
              variant="ghost"
              disabled={adjust.isPending || first >= last}
              onClick={() => move(first + 1, last)}
              aria-label={m.shorts.startLater}
            >
              <ChevronRightIcon />
            </Button>
          </span>
          <span className="flex items-center gap-0.5">
            {m.shorts.end}
            <Button
              size="icon-xs"
              variant="ghost"
              disabled={adjust.isPending || last <= first}
              onClick={() => move(first, last - 1)}
              aria-label={m.shorts.endEarlier}
            >
              <ChevronLeftIcon />
            </Button>
            <Button
              size="icon-xs"
              variant="ghost"
              disabled={adjust.isPending || last >= sentences.length - 1}
              onClick={() => move(first, last + 1)}
              aria-label={m.shorts.endLater}
            >
              <ChevronRightIcon />
            </Button>
          </span>
          <Button
            size="xs"
            variant="ghost"
            className="ml-auto"
            disabled={discard.isPending}
            onClick={() =>
              discard.mutate(x.id, {
                onError: (err) =>
                  toast.error(m.shorts.discardFailed, {
                    description: errorMessage(err),
                  }),
              })
            }
          >
            <XIcon data-icon="inline-start" />
            {m.shorts.discard}
          </Button>
        </div>
      ) : null}
    </li>
  )
}

/**
 * The long video's narration, sentence by sentence: click the first
 * sentence of a moment, then its last, and add it.
 */
function SentencePicker({
  workspaceId,
  story,
  deliverable: d,
  sentences,
  limits,
  onSeek,
}: {
  workspaceId: string
  story: Story
  deliverable: Deliverable
  sentences: Sentence[]
  limits: Moments["limits"]
  onSeek?: (seconds: number) => void
}) {
  const [range, setRange] = useState<{ from: number; to: number }>()
  const [hook, setHook] = useState("")
  const add = useAddMoment(workspaceId, story.id, d.id)

  const pick = (i: number) => {
    onSeek?.(sentences[i].startSeconds)
    setRange((r) =>
      !r || r.from !== r.to || i < r.from
        ? { from: i, to: i }
        : { from: r.from, to: i }
    )
  }
  const span = range
    ? sentences[range.to].endSeconds - sentences[range.from].startSeconds
    : 0
  const problem = !range
    ? undefined
    : span < limits.clipMinSeconds
      ? m.shorts.tooShort(limits.clipMinSeconds)
      : span > limits.clipMaxSeconds
        ? m.shorts.tooLong(limits.clipMaxSeconds)
        : undefined

  const submit = () => {
    if (!range || problem) return
    add.mutate(
      {
        startSeconds: sentences[range.from].startSeconds,
        endSeconds: sentences[range.to].endSeconds,
        hook: hook.trim() || undefined,
      },
      {
        onSuccess: () => {
          setRange(undefined)
          setHook("")
        },
        onError: (err) =>
          toast.error(m.shorts.addFailed, { description: errorMessage(err) }),
      }
    )
  }

  return (
    <details className="group rounded-xl border">
      <summary className="flex cursor-pointer items-center gap-1.5 rounded-xl px-3 py-2 text-sm font-medium outline-none focus-visible:ring-[3px] focus-visible:ring-ring">
        <PlusIcon className="size-4" aria-hidden="true" />
        {m.shorts.pickTitle}
      </summary>
      <div className="grid gap-3 border-t p-3">
        <p className="text-xs text-muted-foreground">{m.shorts.pickHint}</p>
        <ol
          className="max-h-72 overflow-y-auto rounded-lg border text-sm"
          aria-label={m.shorts.sentencesLabel}
        >
          {sentences.map((s, i) => {
            const inside = !!range && i >= range.from && i <= range.to
            return (
              <li key={i}>
                <button
                  type="button"
                  aria-pressed={inside}
                  onClick={() => pick(i)}
                  className={cn(
                    "flex w-full gap-2 px-2.5 py-1.5 text-left outline-none hover:bg-muted/40 focus-visible:ring-[3px] focus-visible:ring-ring focus-visible:ring-inset",
                    inside && "bg-secondary inset-ring inset-ring-foreground/60"
                  )}
                >
                  <span className="shrink-0 pt-px font-mono text-xs text-muted-foreground tabular-nums">
                    {formatTimestamp(s.startSeconds)}
                  </span>
                  <span>{s.text}</span>
                </button>
              </li>
            )
          })}
        </ol>
        {range ? (
          <p
            className={cn(
              "text-xs",
              problem ? "text-destructive" : "text-muted-foreground"
            )}
          >
            {clipRange({
              startSeconds: sentences[range.from].startSeconds,
              endSeconds: sentences[range.to].endSeconds,
            })}
            {" · "}
            {problem ?? m.shorts.seconds(Math.round(span))}
          </p>
        ) : null}
        <div className="flex gap-2">
          <Input
            value={hook}
            onChange={(e) => setHook(e.target.value)}
            maxLength={120}
            placeholder={m.shorts.hookPlaceholder}
            aria-label={m.shorts.hookLabel}
          />
          <Button
            size="default"
            variant="outline"
            disabled={!range || !!problem || add.isPending}
            onClick={submit}
          >
            {m.shorts.add}
          </Button>
        </div>
      </div>
    </details>
  )
}

/** Server messages are lower-case fragments; show them as sentences. */
function sentence(s: string): string {
  const t = s.trim()
  if (!t) return t
  const capital = t[0].toUpperCase() + t.slice(1)
  return /[.!?]$/.test(capital) ? capital : `${capital}.`
}
