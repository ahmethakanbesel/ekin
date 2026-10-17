import type { Job, JobStatus } from "@/api/client"

/** What a step does, for its label and colour. */
export type Phase =
  | "plan"
  | "draft"
  | "render"
  | "final"
  | "publish"
  | "extract"
  | "seo"
  | "template"
  | "voice"
  | "other"

/** Where a request's time went: a phase running, queued work, or nothing. */
export type Bucket = Phase | "queued" | "idle"

export const PHASES: Phase[] = [
  "plan",
  "draft",
  "render",
  "final",
  "publish",
  "extract",
  "seo",
  "template",
  "voice",
  "other",
]

export function phaseOf(job: Job): Phase {
  switch (job.kind) {
    case "story.adhoc":
    case "story.moments":
    case "pipeline.run":
      return "plan"
    case "deliverable.draft":
      return "draft"
    case "deliverable.render":
      return job.subject.final ? "final" : "render"
    case "deliverable.publish":
      return "publish"
    case "knowledge.extract":
      return "extract"
    case "seo.refresh":
      return "seo"
    case "template.generate":
      return "template"
    case "voice.preview":
      return "voice"
  }
  return "other"
}

/** One job of a request, with its times in epoch milliseconds. */
export type Step = {
  job: Job
  phase: Phase
  created: number
  /** When its latest attempt began; undefined before the first run (or for jobs recorded before starts were). */
  began?: number
  /** When it finished, or now while it is queued or running. */
  end: number
  active: boolean
}

export type Request = {
  id: string
  /** Oldest first. */
  steps: Step[]
  /** The job that started the request, when it is in the list. */
  root?: Job
  status: JobStatus
  active: boolean
  start: number
  end: number
  /** Milliseconds per bucket; they add up to end − start. */
  spent: Map<Bucket, number>
  /** Time with at least one step running. */
  busy: number
}

const ACTIVE: JobStatus[] = ["queued", "running"]

/** How long a step waited for a worker; undefined when unknown. */
export function waited(s: Step): number | undefined {
  if (s.began !== undefined) return Math.max(0, s.began - s.created)
  if (s.job.status === "queued") return Math.max(0, s.end - s.created)
  return undefined
}

/** How long a step ran. Without a recorded start, its whole span. */
export function ran(s: Step): number | undefined {
  if (s.began !== undefined) return Math.max(0, s.end - s.began)
  if (s.job.status === "queued" || s.job.status === "cancelled") return undefined
  return Math.max(0, s.end - s.created)
}

function stepOf(job: Job, now: number): Step {
  const active = ACTIVE.includes(job.status)
  const created = Date.parse(job.createdAt)
  const finished = job.finishedAt ? Date.parse(job.finishedAt) : undefined
  const began = job.startedAt && job.status !== "queued" ? Date.parse(job.startedAt) : undefined
  return {
    job,
    phase: phaseOf(job),
    created,
    began,
    end: active ? Math.max(now, created) : (finished ?? Date.parse(job.updatedAt)),
    active,
  }
}

/**
 * The request's outcome: running or queued while any step is, else the
 * worst final state among the latest attempt of each step (a failed
 * draft retried successfully counts as succeeded).
 */
function statusOf(steps: Step[]): JobStatus {
  if (steps.some((s) => s.job.status === "running")) return "running"
  if (steps.some((s) => s.job.status === "queued")) return "queued"
  const latest = new Map<string, JobStatus>()
  for (const s of steps) {
    const subject = s.job.subject
    latest.set(
      `${s.phase}/${subject.deliverableId ?? subject.documentId ?? s.job.id}`,
      s.job.status
    )
  }
  const outcomes = [...latest.values()]
  if (outcomes.includes("failed")) return "failed"
  if (outcomes.includes("cancelled")) return "cancelled"
  return "succeeded"
}

/**
 * Splits the request's time into buckets. Each moment goes to the
 * phases running then (shared equally), else to "queued" when a step
 * waits for a worker, else to "idle" (waiting for a reviewer, or for
 * nothing).
 */
function spentOf(
  steps: Step[],
  start: number,
  end: number
): { spent: Map<Bucket, number>; busy: number } {
  type Span = { from: number; to: number; bucket: Bucket }
  const spans: Span[] = []
  for (const s of steps) {
    const runFrom = s.began ?? (ran(s) !== undefined ? s.created : undefined)
    const queuedTo = runFrom ?? (s.job.status === "queued" ? s.end : s.created)
    if (queuedTo > s.created) spans.push({ from: s.created, to: queuedTo, bucket: "queued" })
    if (runFrom !== undefined && s.end > runFrom)
      spans.push({ from: runFrom, to: s.end, bucket: s.phase })
  }
  const cuts = [...new Set([start, end, ...spans.flatMap((s) => [s.from, s.to])])]
    .filter((t) => t >= start && t <= end)
    .toSorted((a, b) => a - b)
  const spent = new Map<Bucket, number>()
  const add = (b: Bucket, ms: number) => spent.set(b, (spent.get(b) ?? 0) + ms)
  let busy = 0
  for (let i = 0; i + 1 < cuts.length; i++) {
    const from = cuts[i]
    const to = cuts[i + 1]
    const open = spans.filter((s) => s.from <= from && s.to >= to)
    const running = open.filter((s) => s.bucket !== "queued")
    if (running.length > 0) {
      busy += to - from
      for (const s of running) add(s.bucket, (to - from) / running.length)
    } else if (open.length > 0) add("queued", to - from)
    else add("idle", to - from)
  }
  return { spent, busy }
}

/** Groups jobs into requests by trace, the most recently active first. */
export function groupRequests(jobs: Job[], now: number): Request[] {
  const byTrace = new Map<string, Job[]>()
  for (const j of jobs) {
    const list = byTrace.get(j.traceId)
    if (list) list.push(j)
    else byTrace.set(j.traceId, [j])
  }
  const out: Request[] = []
  for (const [id, group] of byTrace) {
    const steps = group.map((j) => stepOf(j, now)).toSorted((a, b) => a.created - b.created)
    const start = steps[0].created
    const end = Math.max(...steps.map((s) => s.end))
    const { spent, busy } = spentOf(steps, start, end)
    out.push({
      id,
      steps,
      root: group.find((j) => j.id === id),
      status: statusOf(steps),
      active: steps.some((s) => s.active),
      start,
      end,
      spent,
      busy,
    })
  }
  const latest = (r: Request) => r.steps[r.steps.length - 1].created
  return out.toSorted((a, b) => latest(b) - latest(a))
}
