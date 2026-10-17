import type { JobStatus } from "@/api/client"
import { QUEUE_ORDER, type Queue } from "@/lib/tones"

export const JOB_STATUSES: JobStatus[] = ["queued", "running", "succeeded", "failed", "cancelled"]

export type JobsSearch = { status?: string; queue?: Queue }

/** `?status=queued,running&queue=render` — unknown values are dropped. */
export function validateJobsSearch(search: Record<string, unknown>): JobsSearch {
  const raw = typeof search.status === "string" ? search.status : undefined
  const status = serializeStatusFilter(parseStatusFilter(raw))
  const queue = parseQueueFilter(search.queue)
  return { ...(status ? { status } : {}), ...(queue ? { queue } : {}) }
}

export function parseStatusFilter(raw: string | undefined): JobStatus[] {
  if (!raw) return []
  const wanted = new Set(raw.split(","))
  return JOB_STATUSES.filter((s) => wanted.has(s))
}

export function serializeStatusFilter(status: JobStatus[]): string | undefined {
  const ordered = JOB_STATUSES.filter((s) => status.includes(s))
  return ordered.length ? ordered.join(",") : undefined
}

/** One queue to show requests that touched it (see QUEUE_KINDS). */
export function parseQueueFilter(raw: unknown): Queue | undefined {
  return QUEUE_ORDER.find((q) => q === raw)
}

/**
 * The job kinds each queue runs (the server's job.Spec queues). The API
 * filters by kind, so the queue filter asks for the requests with a job
 * of these kinds. A new kind must be added here to be found by queue.
 */
export const QUEUE_KINDS: Record<Queue, string[]> = {
  control: ["story.adhoc", "pipeline.run"],
  agent: ["deliverable.draft", "seo.refresh", "template.generate"],
  tts: ["voice.preview"],
  render: ["deliverable.render"],
  extract: ["knowledge.extract"],
  io: ["deliverable.publish"],
}
