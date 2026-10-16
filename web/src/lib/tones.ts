import type { DeliverableStatus, JobStatus } from "@/api/client"

/*
 * Colour roles, all drawn from theme tokens (index.css):
 * info = the system is working (violet), attention = the primary blue, it needs a person,
 * success = done, danger = failed, neutral/muted = waiting or set aside.
 */
export type Tone =
  "info" | "attention" | "success" | "danger" | "neutral" | "muted"

/** Badge classes per tone, laid over the stock Badge. */
export const badgeTone: Record<Tone, string> = {
  info: "bg-info/15 text-info",
  attention:
    "bg-primary text-primary-foreground inset-ring inset-ring-primary-ink",
  success: "bg-success/15 text-success",
  danger: "bg-destructive/10 text-destructive dark:bg-destructive/15",
  neutral: "border-border bg-transparent text-foreground",
  muted: "border-border bg-transparent text-muted-foreground",
}

const DELIVERABLE: Record<DeliverableStatus, Tone> = {
  waiting: "neutral",
  queued: "neutral",
  drafting: "info",
  rendering: "info",
  review: "attention",
  finalizing: "info",
  ready: "success",
  publishing: "info",
  published: "success",
  rejected: "muted",
  failed: "danger",
}

export function deliverableTone(status: DeliverableStatus): Tone {
  return DELIVERABLE[status]
}

/** Badge classes for a deliverable status; a rejected one is struck through. */
export function deliverableBadge(status: DeliverableStatus): string {
  const tone = badgeTone[DELIVERABLE[status]]
  return status === "rejected" ? `${tone} line-through decoration-1` : tone
}

const JOB: Record<JobStatus, Tone> = {
  queued: "neutral",
  running: "info",
  succeeded: "success",
  failed: "danger",
  cancelled: "muted",
}

export function jobTone(status: JobStatus): Tone {
  return JOB[status]
}

/** Worker queues (job.v1 Queue). */
export type Queue = "control" | "agent" | "tts" | "render" | "extract" | "io"

/** Fill per queue, for dots, swatches and bars; control is plumbing, so muted. */
export const queueTone: Record<Queue, string> = {
  control: "bg-muted-foreground",
  agent: "bg-chart-1",
  tts: "bg-chart-2",
  render: "bg-chart-3",
  extract: "bg-chart-4",
  io: "bg-chart-5",
}

/** All queues, in the order the throttle and system pages list them. */
export const QUEUE_ORDER = Object.keys(queueTone) as Queue[]

/** A job's queue fill; a queue this build does not know gets the muted tone. */
export function queueFill(queue: string): string {
  return queueTone[queue as Queue] ?? queueTone.control
}

/** The queue each jobs-page phase runs on, so its bars take that queue's hue. */
export const phaseQueue = {
  plan: "control",
  draft: "agent",
  render: "render",
  final: "render",
  publish: "io",
  extract: "extract",
  seo: "agent",
  template: "agent",
  voice: "tts",
  other: "control",
} as const satisfies Record<string, Queue>

const PIPELINE_BG = [
  "bg-pipeline-1",
  "bg-pipeline-2",
  "bg-pipeline-3",
  "bg-pipeline-4",
  "bg-pipeline-5",
]

/**
 * A stable identity hue for a pipeline, hashed from its id (there is no colour
 * field). Ad-hoc stories, with no pipeline, get the muted tone.
 */
export function pipelineHue(id?: string | null): { bg: string; color: string } {
  if (!id)
    return { bg: "bg-muted-foreground", color: "var(--muted-foreground)" }
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0
  const n = h % PIPELINE_BG.length
  return { bg: PIPELINE_BG[n], color: `var(--pipeline-${n + 1})` }
}

/** Banner and callout roles: surface, and the colour of its leading icon. */
export type BannerTone = "working" | "attention" | "done" | "danger" | "muted"

export const toneClass: Record<BannerTone, string> = {
  working: "border-info/30 bg-info/5",
  attention: "border-primary-ink/40 bg-primary/10",
  done: "border-success/30 bg-success/5",
  danger: "border-destructive/30 bg-destructive/5",
  muted: "bg-muted/40",
}

export const iconClass: Record<BannerTone, string> = {
  working: "text-info",
  attention: "text-primary-ink",
  done: "text-success",
  danger: "text-destructive",
  muted: "text-muted-foreground",
}
