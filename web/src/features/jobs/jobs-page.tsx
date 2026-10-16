import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import {
  BanIcon,
  ChevronRightIcon,
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  CircleCheckIcon,
  CircleXIcon,
  ClockIcon,
  ListChecksIcon,
  Loader2Icon,
  XIcon,
  type LucideIcon,
} from "lucide-react"
import { toast } from "sonner"

import type { Document, Job, JobStatus, Pipeline, Story } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  PageHeader,
  TableSkeleton,
} from "@/components/page"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, formatSpan, useNow } from "@/lib/time"
import {
  QUEUE_ORDER,
  badgeTone,
  jobTone,
  phaseQueue,
  queueFill,
  queueTone,
  type Queue,
} from "@/lib/tones"
import { useRetained } from "@/lib/use-retained"
import { cn } from "@/lib/utils"
import { documentsQuery } from "@/queries/documents"
import { JOB_LIST_LIMIT, jobsQuery, useCancelJob } from "@/queries/jobs"
import { pipelinesQuery } from "@/queries/pipelines"
import { storiesQuery } from "@/queries/stories"
import {
  PHASES,
  groupRequests,
  ran,
  waited,
  type Bucket,
  type Phase,
  type Request,
  type Step,
} from "./requests"
import { QueueBadge, QueueDot } from "./queue"
import {
  JOB_STATUSES,
  parseQueueFilter,
  parseStatusFilter,
  serializeStatusFilter,
} from "./search"
import {
  PausedBanners,
  ThrottleControl,
  ThrottlePanel,
  useThrottle,
} from "./throttle"

const statusIcon: Record<JobStatus, LucideIcon> = {
  queued: ClockIcon,
  running: Loader2Icon,
  succeeded: CircleCheckIcon,
  failed: CircleXIcon,
  cancelled: BanIcon,
}

function JobStatusBadge({
  status,
  className,
}: {
  status: JobStatus
  className?: string
}) {
  const Icon = statusIcon[status]
  return (
    <Badge
      variant="secondary"
      className={cn(badgeTone[jobTone(status)], className)}
    >
      <Icon
        data-icon="inline-start"
        className={cn(status === "running" && "animate-spin")}
      />
      {m.jobs.status[status]}
    </Badge>
  )
}

/** Phases take their queue's hue; queued time is hatched, idle time a faint fill. */
const bucketTone: Record<Bucket, string> = {
  ...(Object.fromEntries(
    PHASES.map((p) => [p, queueTone[phaseQueue[p]]])
  ) as Record<Phase, string>),
  queued:
    "bg-[repeating-linear-gradient(135deg,var(--muted-foreground)_0_1.5px,transparent_1.5px_3.5px)]",
  idle: "bg-muted-foreground/10",
}

/** The filters' "everything" item. */
const ALL = "all"

/** Buckets in chart order: the work, then the waiting. */
const BUCKETS: Bucket[] = [...PHASES, "queued", "idle"]

/** Names for what requests work on: stories, pipelines and documents. */
type Lookup = {
  stories: Map<string, Story>
  pipelines: Map<string, Pipeline>
  documents: Map<string, Document>
}

export function JobsPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: "/w/$workspaceId/jobs" })
  const navigate = useNavigate({ from: "/w/$workspaceId/jobs" })
  const status = parseStatusFilter(search.status)
  const queue = parseQueueFilter(search.queue)
  const jobs = useQuery({
    ...jobsQuery(workspace.id, status),
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  })
  const stories = useQuery(storiesQuery(workspace.id, []))
  const pipelines = useQuery(pipelinesQuery(workspace.id))
  const documents = useQuery(documentsQuery(workspace.id))
  const now = useNow(5_000)
  const [cancelling, setCancelling] = useState<Job | null>(null)
  const throttle = useThrottle() // admins only
  // Requests the viewer opened or closed; the rest are open while at work.
  const [folds, setFolds] = useState<Record<string, boolean>>({})

  const lookup = useMemo<Lookup>(
    () => ({
      stories: new Map((stories.data ?? []).map((s) => [s.id, s])),
      pipelines: new Map((pipelines.data ?? []).map((p) => [p.id, p])),
      documents: new Map((documents.data ?? []).map((d) => [d.id, d])),
    }),
    [stories.data, pipelines.data, documents.data]
  )
  // Grouped from every job, so a request keeps all its steps; the queue
  // filter then keeps the requests that touched that queue.
  const requests = useMemo(() => {
    const all = groupRequests(jobs.data ?? [], now)
    return queue
      ? all.filter((r) => r.steps.some((s) => s.job.queue === queue))
      : all
  }, [jobs.data, now, queue])
  const isOpen = (r: Request) => folds[r.id] ?? r.active
  const anyOpen = requests.some(isOpen)

  const setStatus = (next: JobStatus[]) =>
    void navigate({
      search: (prev) => ({ ...prev, status: serializeStatusFilter(next) }),
      replace: true,
    })

  const setQueue = (next: Queue | undefined) =>
    void navigate({
      search: (prev) => ({ ...prev, queue: next }),
      replace: true,
    })

  // "All" clears the other statuses; picking a status clears "All".
  const onStatus = (next: string[]) => {
    if (next.includes(ALL) && status.length > 0) setStatus([])
    else setStatus(JOB_STATUSES.filter((s) => next.includes(s)))
  }

  const setAll = (open: boolean) =>
    setFolds(Object.fromEntries(requests.map((r) => [r.id, open])))

  let body
  if (jobs.isPending) body = <TableSkeleton rows={6} />
  else if (jobs.isError)
    body = <ErrorState error={jobs.error} onRetry={() => void jobs.refetch()} />
  else if (requests.length === 0)
    body = (
      <EmptyState
        icon={<ListChecksIcon />}
        title={m.jobs.emptyTitle}
        body={
          status.length || queue ? m.jobs.emptyFiltered : m.jobs.emptyBody
        }
      />
    )
  else
    body = (
      <>
        <div className="divide-y rounded-xl border">
          {requests.map((r) => (
            <RequestRow
              key={r.id}
              request={r}
              lookup={lookup}
              workspaceId={workspace.id}
              timezone={workspace.timezone}
              now={now}
              open={isOpen(r)}
              onToggle={() => setFolds((f) => ({ ...f, [r.id]: !isOpen(r) }))}
              canCancel={canEdit}
              onCancel={setCancelling}
            />
          ))}
        </div>
        {requests.length >= JOB_LIST_LIMIT ? (
          <p className="mt-3 text-xs text-muted-foreground">
            {m.jobs.showing(JOB_LIST_LIMIT)}
          </p>
        ) : null}
      </>
    )

  return (
    <>
      <PageHeader
        title={m.jobs.title}
        description={m.jobs.description}
        actions={
          throttle ? (
            <ThrottleControl throttle={throttle} className="min-[90rem]:hidden" />
          ) : null
        }
      />
      {throttle ? <PausedBanners throttle={throttle} /> : null}
      {/* From 1440px the throttle stays open beside the list (below that the
          step tables would scroll); narrower screens use the header popover. */}
      <div
        className={cn(
          throttle &&
            "min-[90rem]:grid min-[90rem]:grid-cols-[minmax(0,1fr)_20rem] min-[90rem]:items-start min-[90rem]:gap-6"
        )}
      >
        <div className="min-w-0">
          <div className="mb-4 flex flex-wrap items-start gap-x-4 gap-y-2">
            <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-2">
              <span className="text-sm text-muted-foreground" id="jobs-status">
                {m.jobs.columns.status}
              </span>
              <div className="scroll-area min-w-0 overflow-x-auto">
                <ToggleGroup
                  multiple
                  variant="outline"
                  size="sm"
                  spacing={0}
                  aria-labelledby="jobs-status"
                  value={status.length ? status : [ALL]}
                  onValueChange={onStatus}
                >
                  <ToggleGroupItem value={ALL}>{m.jobs.all}</ToggleGroupItem>
                  {JOB_STATUSES.map((s) => (
                    <ToggleGroupItem key={s} value={s}>
                      {m.jobs.status[s]}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </div>
              <span className="text-sm text-muted-foreground" id="jobs-queue">
                {m.jobs.columns.queue}
              </span>
              <div className="scroll-area min-w-0 overflow-x-auto">
                <ToggleGroup
                  variant="outline"
                  size="sm"
                  spacing={0}
                  aria-labelledby="jobs-queue"
                  value={[queue ?? ALL]}
                  onValueChange={(next) => setQueue(parseQueueFilter(next[0]))}
                >
                  <ToggleGroupItem value={ALL}>{m.jobs.all}</ToggleGroupItem>
                  {QUEUE_ORDER.map((q) => (
                    <ToggleGroupItem
                      key={q}
                      value={q}
                      title={m.system.queues[q]}
                    >
                      <QueueDot queue={q} />
                      {q}
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </div>
            </div>
            {requests.length > 0 ? (
              <Button
                variant="ghost"
                size="sm"
                className="ml-auto"
                onClick={() => setAll(!anyOpen)}
              >
                {anyOpen ? <ChevronsDownUpIcon /> : <ChevronsUpDownIcon />}
                {anyOpen ? m.jobs.request.collapseAll : m.jobs.request.expandAll}
              </Button>
            ) : null}
          </div>
          {body}
        </div>
        {throttle ? (
          <ThrottlePanel throttle={throttle} className="hidden min-[90rem]:flex" />
        ) : null}
      </div>
      <CancelJobDialog
        job={cancelling}
        onOpenChange={(open) => !open && setCancelling(null)}
      />
    </>
  )
}

/** The request's title, where it came from, and the story it made (to link). */
function describe(
  r: Request,
  lookup: Lookup
): { title: string; origin: string; story?: Story; more: number } {
  const t = m.jobs.request.origin
  const storyIds = [
    ...new Set(r.steps.flatMap((s) => s.job.subject.storyId ?? [])),
  ]
  const story = storyIds
    .map((id) => lookup.stories.get(id))
    .find((s) => s !== undefined)
  const root = r.root ?? r.steps[0].job
  const pipelineId = root.subject.pipelineId ?? story?.pipelineId
  const pipeline = pipelineId
    ? lookup.pipelines.get(pipelineId)?.definition.name
    : undefined
  const documentId = r.steps.find((s) => s.job.subject.documentId)?.job.subject
    .documentId
  const document = documentId
    ? lookup.documents.get(documentId)?.title
    : undefined

  let origin: string
  if (root.kind === "story.adhoc" || story?.origin === "adhoc") origin = t.adhoc
  else if (pipeline) origin = t.run(pipeline)
  else if (root.kind === "pipeline.run") origin = t.pipeline
  else if (document) origin = t.document(document)
  else origin = m.jobs.phase[r.steps[0].phase]

  const title =
    story?.title ?? document ?? pipeline ?? m.jobs.phase[r.steps[0].phase]
  return { title, origin, story, more: Math.max(0, storyIds.length - 1) }
}

function RequestRow({
  request: r,
  lookup,
  workspaceId,
  timezone,
  now,
  open,
  onToggle,
  canCancel,
  onCancel,
}: {
  request: Request
  lookup: Lookup
  workspaceId: string
  timezone: string
  now: number
  open: boolean
  onToggle: () => void
  canCancel: boolean
  onCancel: (job: Job) => void
}) {
  const { title, origin, story, more } = describe(r, lookup)
  const t = m.jobs.request
  const startIso = new Date(r.start).toISOString()
  return (
    <section>
      <div className="relative flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 hover:bg-muted/40">
        {/* The toggle covers the whole header; the story link sits above it. */}
        <button
          type="button"
          aria-expanded={open}
          aria-label={open ? t.collapse(title) : t.expand(title)}
          onClick={onToggle}
          className="grid size-6 shrink-0 place-items-center rounded-full text-muted-foreground outline-none after:absolute after:inset-0 after:content-[''] focus-visible:ring-[3px] focus-visible:ring-ring"
        >
          <ChevronRightIcon
            className={cn("size-4 transition-transform", open && "rotate-90")}
          />
        </button>
        <div className="flex min-w-0 flex-1 basis-56 flex-col gap-0.5">
          <span className="flex min-w-0 items-baseline gap-2">
            {story ? (
              <Link
                to="/w/$workspaceId/stories/$storyId"
                params={{ workspaceId, storyId: story.id }}
                className="relative z-10 truncate font-medium hover:underline"
              >
                {title}
              </Link>
            ) : (
              <span className="truncate font-medium">{title}</span>
            )}
            {more > 0 ? (
              <span className="shrink-0 text-xs text-muted-foreground">
                {t.more(more)}
              </span>
            ) : null}
          </span>
          <span className="truncate text-xs text-muted-foreground">
            {origin} · {t.steps(r.steps.length)} ·{" "}
            <span title={formatDateTime(startIso, timezone)}>
              {formatRelative(startIso, now)}
            </span>
          </span>
        </div>
        <JobStatusBadge status={r.status} className="shrink-0" />
        <TimeBar request={r} className="w-full sm:w-56" />
        <div className="flex w-28 shrink-0 flex-col items-end gap-0.5 text-right">
          <span className="tabular text-sm font-medium">
            {formatSpan(r.end - r.start)}
          </span>
          <span className="tabular text-[11px] text-muted-foreground">
            {t.busy(formatSpan(r.busy))}
          </span>
        </div>
      </div>
      {open ? (
        <div className="border-t bg-muted/20 px-4 py-3">
          <Breakdown request={r} />
          <Steps
            request={r}
            lookup={lookup}
            timezone={timezone}
            canCancel={canCancel}
            onCancel={onCancel}
          />
        </div>
      ) : null}
    </section>
  )
}

/** The request's time stacked by bucket: each phase's work, then waiting. */
function TimeBar({
  request: r,
  className,
}: {
  request: Request
  className?: string
}) {
  const total = Math.max(1, r.end - r.start)
  return (
    <div
      className={cn(
        "flex h-2.5 shrink-0 overflow-hidden rounded-full bg-muted",
        className
      )}
      role="img"
      aria-label={summary(r)}
    >
      {BUCKETS.map((b) => {
        const ms = r.spent.get(b) ?? 0
        if (ms <= 0) return null
        return (
          <span
            key={b}
            className={cn("h-full", bucketTone[b])}
            style={{ width: `${(ms / total) * 100}%` }}
            title={`${m.jobs.phase[b]}: ${formatSpan(ms)}`}
          />
        )
      })}
    </div>
  )
}

function summary(r: Request): string {
  return BUCKETS.flatMap((b) => {
    const ms = r.spent.get(b) ?? 0
    return ms > 0 ? [`${m.jobs.phase[b]} ${formatSpan(ms)}`] : []
  }).join(", ")
}

function Breakdown({ request: r }: { request: Request }) {
  const total = Math.max(1, r.end - r.start)
  const rows = BUCKETS.flatMap((b) => {
    const ms = r.spent.get(b) ?? 0
    return ms > 0 ? [{ b, ms }] : []
  }).toSorted((a, b) => b.ms - a.ms)
  return (
    <figure className="mb-3">
      <figcaption className="mb-2 text-xs font-medium text-muted-foreground">
        {m.jobs.request.whereTimeWent}
      </figcaption>
      <ul className="grid gap-1.5">
        {rows.map(({ b, ms }) => (
          <li
            key={b}
            className="grid grid-cols-[9rem_1fr_6.5rem] items-center gap-3 text-xs"
          >
            <span className="flex min-w-0 items-center gap-2">
              <span
                className={cn("size-2.5 shrink-0 rounded-sm", bucketTone[b])}
              />
              <span className="truncate">{m.jobs.phase[b]}</span>
            </span>
            <span className="h-2 overflow-hidden rounded-full bg-muted">
              <span
                className={cn("block h-full rounded-full", bucketTone[b])}
                style={{ width: `${Math.max(0.5, (ms / total) * 100)}%` }}
              />
            </span>
            <span className="tabular text-right">
              {formatSpan(ms)}{" "}
              <span className="text-muted-foreground">
                {Math.round((ms / total) * 100)}%
              </span>
            </span>
          </li>
        ))}
      </ul>
    </figure>
  )
}

function stepLabel(s: Step, lookup: Lookup): string {
  const phase = m.jobs.phase[s.phase]
  const { storyId, deliverableId } = s.job.subject
  const d = storyId
    ? lookup.stories
        .get(storyId)
        ?.deliverables.find((x) => x.id === deliverableId)
    : undefined
  if (!d) return phase
  return `${phase} · ${m.values.kind[d.kind] ?? d.kind} ${d.language.toUpperCase()}`
}

function Steps({
  request: r,
  lookup,
  timezone,
  canCancel,
  onCancel,
}: {
  request: Request
  lookup: Lookup
  timezone: string
  canCancel: boolean
  onCancel: (job: Job) => void
}) {
  const t = m.jobs.request
  return (
    <div className="rounded-lg border bg-background">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="min-w-40 pl-4">{t.step}</TableHead>
            <TableHead>{m.jobs.columns.queue}</TableHead>
            <TableHead>{m.jobs.columns.status}</TableHead>
            <TableHead className="text-right">
              {m.jobs.columns.attempt}
            </TableHead>
            <TableHead className="text-right">{t.waited}</TableHead>
            <TableHead className="text-right">{t.ran}</TableHead>
            <TableHead className="w-full min-w-28">{t.timeline}</TableHead>
            <TableHead className="w-12 pr-4">
              <span className="sr-only">{m.common.actions}</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {r.steps.map((s) => {
            const job = s.job
            const wait = waited(s)
            const run = ran(s)
            const cancellable = canCancel && s.active
            return (
              <TableRow key={job.id}>
                <TableCell className="max-w-72 pl-4 align-top whitespace-normal">
                  <div className="flex flex-col gap-0.5">
                    <span className="text-sm">{stepLabel(s, lookup)}</span>
                    <span
                      className="font-mono text-[11px] text-muted-foreground"
                      title={`${job.kind} · ${job.id}`}
                    >
                      {job.id.slice(0, 8)}
                    </span>
                    {job.lastError ? (
                      <Tooltip>
                        <TooltipTrigger
                          render={
                            <span className="line-clamp-2 cursor-default text-xs break-words text-destructive" />
                          }
                        >
                          {job.lastError}
                        </TooltipTrigger>
                        <TooltipContent className="max-w-md whitespace-pre-wrap">
                          {job.lastError}
                        </TooltipContent>
                      </Tooltip>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell className="align-top">
                  <QueueBadge queue={job.queue} />
                </TableCell>
                <TableCell className="align-top">
                  <JobStatusBadge status={job.status} />
                </TableCell>
                <TableCell className="tabular text-right align-top">
                  {job.attempt}/{job.maxAttempts}
                </TableCell>
                <TableCell className="tabular text-right align-top text-muted-foreground">
                  <Span ms={wait} />
                </TableCell>
                <TableCell className="tabular text-right align-top font-medium">
                  <Span ms={run} />
                </TableCell>
                <TableCell className="align-top">
                  <Gantt step={s} request={r} timezone={timezone} />
                </TableCell>
                <TableCell className="pr-4 align-top">
                  {cancellable ? (
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={m.jobs.cancel}
                      title={m.jobs.cancel}
                      onClick={() => onCancel(job)}
                    >
                      <XIcon />
                    </Button>
                  ) : null}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

function Span({ ms }: { ms: number | undefined }) {
  if (ms === undefined)
    return (
      <span className="text-muted-foreground" title={m.jobs.request.unknown}>
        {m.common.none}
      </span>
    )
  return <>{formatSpan(ms)}</>
}

/** The step on the request's timeline: its wait for a worker, then its run. */
function Gantt({
  step: s,
  request: r,
  timezone,
}: {
  step: Step
  request: Request
  timezone: string
}) {
  const total = Math.max(1, r.end - r.start)
  const pct = (ms: number) => `${(ms / total) * 100}%`
  const wait = waited(s)
  const run = ran(s)
  const runFrom = s.began ?? (run !== undefined ? s.created : undefined)
  const t = m.jobs.request
  return (
    <div
      className="relative mt-1.5 h-2.5 w-full rounded-full bg-muted/60"
      title={`${formatDateTime(s.job.createdAt, timezone)}${s.job.finishedAt ? ` → ${formatDateTime(s.job.finishedAt, timezone)}` : ""}`}
    >
      {wait !== undefined && wait > 0 ? (
        <span
          className={cn(
            "absolute inset-y-0 min-w-0.5 rounded-full",
            bucketTone.queued
          )}
          style={{ left: pct(s.created - r.start), width: pct(wait) }}
          title={t.waitedFor(formatSpan(wait))}
        />
      ) : null}
      {runFrom !== undefined && run !== undefined ? (
        <span
          className={cn(
            "absolute inset-y-0 min-w-0.5 rounded-full",
            queueFill(s.job.queue),
            s.job.status === "running" && "animate-pulse",
            s.job.status === "failed" &&
              "ring-2 ring-destructive ring-offset-1 ring-offset-background"
          )}
          style={{ left: pct(runFrom - r.start), width: pct(run) }}
          title={t.ranFor(formatSpan(run))}
        />
      ) : null}
    </div>
  )
}

function CancelJobDialog({
  job,
  onOpenChange,
}: {
  job: Job | null
  onOpenChange: (open: boolean) => void
}) {
  const target = useRetained(job)
  const cancel = useCancelJob(target?.workspaceId ?? "")

  const confirm = async () => {
    if (!target) return
    try {
      await cancel.mutateAsync(target.id)
      toast.success(m.jobs.cancelled)
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <AlertDialog open={!!job} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.jobs.cancelTitle}</AlertDialogTitle>
          <AlertDialogDescription>
            {m.jobs.cancelBody(target?.kind ?? "")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.jobs.keep}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={cancel.isPending}
            onClick={() => void confirm()}
          >
            {m.jobs.cancelConfirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
