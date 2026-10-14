import { useEffect, useSyncExternalStore } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import type { Template } from "@/api/client"
import { m } from "@/i18n"
import { keys } from "@/queries/keys"
import { jobQuery, templatesQuery } from "@/queries/templates"

// After "Draft from a channel" queues its job, the workspace keeps an eye
// out for the result: the template list (and the job) are polled every
// 10 seconds for up to 10 minutes, wherever in the workspace you are, and
// a toast says when the draft arrives or fails.

export const DRAFT_POLL_MS = 10_000
const DRAFT_WATCH_MS = 10 * 60_000

export type DraftWatch = {
  jobId: string
  channel: string
  /** Generated templates that existed when the job was queued. */
  known: string[]
  until: number
}

const watches = new Map<string, DraftWatch[]>()
const listeners = new Set<() => void>()
const EMPTY: DraftWatch[] = []

function emit() {
  for (const l of listeners) l()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** Starts watching for the template a just-queued job drafts. */
export function watchDraft(
  workspaceId: string,
  jobId: string,
  channel: string,
  current: Template[] | undefined
) {
  const known = (current ?? []).filter((t) => t.source === "generated").map((t) => t.id)
  const w: DraftWatch = { jobId, channel, known, until: Date.now() + DRAFT_WATCH_MS }
  watches.set(workspaceId, [...(watches.get(workspaceId) ?? []), w])
  emit()
}

function unwatch(workspaceId: string, jobId: string) {
  const next = (watches.get(workspaceId) ?? []).filter((w) => w.jobId !== jobId)
  if (next.length) watches.set(workspaceId, next)
  else watches.delete(workspaceId)
  emit()
}

/** The drafts the workspace is waiting for, oldest first. */
export function useDraftWatches(workspaceId: string): DraftWatch[] {
  return useSyncExternalStore(subscribe, () => watches.get(workspaceId) ?? EMPTY)
}

/** Mounted once per workspace (in its layout): polls and toasts. Renders nothing. */
export function TemplateDraftWatcher({ workspaceId }: { workspaceId: string }) {
  const pending = useDraftWatches(workspaceId)
  const active = pending.length > 0
  const navigate = useNavigate()
  const list = useQuery({
    ...templatesQuery(workspaceId),
    enabled: active,
    refetchInterval: active ? DRAFT_POLL_MS : false,
    refetchIntervalInBackground: true,
  })

  // A generated template no watch knew about: the oldest watch's draft.
  useEffect(() => {
    if (!list.data) return
    const generated = list.data.filter((t) => t.source === "generated")
    const claimed = new Set<string>()
    for (const w of pending) {
      const fresh = generated.find((t) => !w.known.includes(t.id) && !claimed.has(t.id))
      if (!fresh) continue
      claimed.add(fresh.id)
      unwatch(workspaceId, w.jobId)
      toast.success(m.templates.generate.arrived(fresh.name), {
        description: m.templates.generate.arrivedBody,
        duration: 15_000,
        action: {
          label: m.templates.generate.open,
          onClick: () =>
            void navigate({
              to: "/w/$workspaceId/templates/$templateId",
              params: { workspaceId, templateId: fresh.id },
            }),
        },
      })
    }
  }, [list.data, pending, workspaceId, navigate])

  // Give up quietly-ish after ten minutes.
  useEffect(() => {
    if (!active) return
    const next = Math.min(...pending.map((w) => w.until))
    const timer = window.setTimeout(
      () => {
        const now = Date.now()
        for (const w of pending) {
          if (w.until > now) continue
          unwatch(workspaceId, w.jobId)
          toast.info(m.templates.generate.timedOut, {
            description: m.templates.generate.timedOutBody,
            action: {
              label: m.templates.generate.viewJobs,
              onClick: () => void navigate({ to: "/w/$workspaceId/jobs", params: { workspaceId } }),
            },
          })
        }
      },
      Math.max(0, next - Date.now())
    )
    return () => window.clearTimeout(timer)
  }, [active, pending, workspaceId, navigate])

  return (
    <>
      {pending.map((w) => (
        <DraftJobWatch key={w.jobId} workspaceId={workspaceId} watch={w} />
      ))}
    </>
  )
}

/** The job's own outcome: a failure ends the wait; success fetches the list now. */
function DraftJobWatch({ workspaceId, watch }: { workspaceId: string; watch: DraftWatch }) {
  const qc = useQueryClient()
  const job = useQuery({
    ...jobQuery(workspaceId, watch.jobId),
    refetchInterval: DRAFT_POLL_MS,
    refetchIntervalInBackground: true,
  })
  const status = job.data?.status
  const lastError = job.data?.lastError

  useEffect(() => {
    if (status === "failed" || status === "cancelled") {
      unwatch(workspaceId, watch.jobId)
      toast.error(m.templates.generate.failed, { description: lastError || undefined })
    } else if (status === "succeeded") {
      void qc.invalidateQueries({ queryKey: keys.templates(workspaceId), exact: true })
    }
  }, [status, lastError, workspaceId, watch.jobId, qc])

  return null
}
