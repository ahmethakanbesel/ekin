import { useSyncExternalStore } from "react"

/** How long an approval can be undone before it is sent. */
export const APPROVE_DELAY_MS = 6_000

/**
 * waiting: can still be undone, until `deadline` (epoch ms).
 * sending: the request is out; it can no longer be undone.
 */
export type PendingApproval =
  | { state: "waiting"; deadline: number }
  | { state: "sending" }

type Entry = {
  view: PendingApproval
  timer: number
  /** Sends the approval; keepalive when the page is going away. */
  commit: (keepalive: boolean) => Promise<void>
  onCancel: () => void
}

// Module state, not component state: the reviewer moves on through the
// sitting (unmounting the rail) while an approval waits to be sent.
const entries = new Map<string, Entry>()
const listeners = new Set<() => void>()
let listening = false

function changed() {
  for (const l of listeners) l()
}

function send(id: string, keepalive: boolean) {
  const e = entries.get(id)
  if (!e || e.view.state !== "waiting") return
  window.clearTimeout(e.timer)
  e.view = { state: "sending" }
  changed()
  void e.commit(keepalive).finally(() => {
    entries.delete(id)
    changed()
  })
}

/** A tab closing or reloading sends what is waiting rather than dropping it. */
function flush() {
  for (const id of entries.keys()) send(id, true)
}

/**
 * Approves deliverable `id` after APPROVE_DELAY_MS unless cancelled first.
 * False when an approval of it is already waiting or being sent.
 */
export function scheduleApproval(
  id: string,
  commit: Entry["commit"],
  onCancel: () => void
): boolean {
  if (entries.has(id)) return false
  if (!listening) {
    window.addEventListener("pagehide", flush)
    listening = true
  }
  entries.set(id, {
    view: { state: "waiting", deadline: Date.now() + APPROVE_DELAY_MS },
    timer: window.setTimeout(() => send(id, false), APPROVE_DELAY_MS),
    commit,
    onCancel,
  })
  changed()
  return true
}

/** Undoes a waiting approval; nothing is sent. False when too late. */
export function cancelApproval(id: string): boolean {
  const e = entries.get(id)
  if (!e || e.view.state !== "waiting") return false
  window.clearTimeout(e.timer)
  entries.delete(id)
  changed()
  e.onCancel()
  return true
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

/** The approval of deliverable `id` waiting or being sent, if any. */
export function usePendingApproval(id: string): PendingApproval | undefined {
  return useSyncExternalStore(subscribe, () => entries.get(id)?.view)
}
