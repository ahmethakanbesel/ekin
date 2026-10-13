import type { JobStatus } from "@/api/client"

export const JOB_STATUSES: JobStatus[] = ["queued", "running", "succeeded", "failed", "cancelled"]

export type JobsSearch = { status?: string }

/** `?status=queued,running` — unknown values are dropped. */
export function validateJobsSearch(search: Record<string, unknown>): JobsSearch {
  const raw = typeof search.status === "string" ? search.status : undefined
  const status = serializeStatusFilter(parseStatusFilter(raw))
  return status ? { status } : {}
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
