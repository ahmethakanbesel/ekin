import { useSyncExternalStore } from "react"

/** What the reviewer decided about one revision of a deliverable. */
export type Decision =
  | { kind: "approved" }
  | { kind: "sent"; notes: number }
  | { kind: "rejected" }

export type LedgerEntry = Decision & {
  storyId: string
  deliverableId: string
  revision: number
}

// Module state: decisions made since the page loaded, so the end of a
// review sitting can sum them up. Nothing here is stored or sent.
let entries: LedgerEntry[] = []
const listeners = new Set<() => void>()

function changed() {
  for (const l of listeners) l()
}

const same = (
  e: LedgerEntry,
  deliverableId: string,
  revision: number
) => e.deliverableId === deliverableId && e.revision === revision

/** Notes a decision on a revision; a later one on the same revision replaces it. */
export function recordDecision(
  storyId: string,
  deliverableId: string,
  revision: number,
  decision: Decision
) {
  entries = [
    ...entries.filter((e) => !same(e, deliverableId, revision)),
    { ...decision, storyId, deliverableId, revision },
  ]
  changed()
}

/** Takes a decision back (an approval undone, or one that failed). */
export function forgetDecision(deliverableId: string, revision: number) {
  const next = entries.filter((e) => !same(e, deliverableId, revision))
  if (next.length === entries.length) return
  entries = next
  changed()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

/** Every decision made since the page loaded, oldest first. */
export function useDecisions(): LedgerEntry[] {
  return useSyncExternalStore(subscribe, () => entries)
}
