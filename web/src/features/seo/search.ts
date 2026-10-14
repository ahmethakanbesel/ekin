import type { OpportunityStatus } from "@/api/client"

export const OPPORTUNITY_TABS = ["open", "used", "dismissed", "all"] as const
export type OpportunityTab = OpportunityStatus | "all"

export type SeoSearch = { status?: Exclude<OpportunityTab, "open"> }

/** `?status=used` — the default tab (open) and unknown values are dropped. */
export function validateSeoSearch(search: Record<string, unknown>): SeoSearch {
  const status = OPPORTUNITY_TABS.find((t) => t === search.status)
  return status && status !== "open" ? { status } : {}
}
