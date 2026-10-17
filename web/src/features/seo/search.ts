import type { OpportunityStatus } from "@/api/client"
import { parseSearchText } from "@/lib/utils"

export const OPPORTUNITY_TABS = ["open", "used", "dismissed", "all"] as const
export type OpportunityTab = OpportunityStatus | "all"

export type SeoSearch = { status?: Exclude<OpportunityTab, "open">; q?: string }

/** `?status=used&q=sınav` — the default tab (open), unknown values and an empty search are dropped. */
export function validateSeoSearch(search: Record<string, unknown>): SeoSearch {
  const status = OPPORTUNITY_TABS.find((t) => t === search.status)
  const q = parseSearchText(search.q)
  return { ...(status && status !== "open" ? { status } : {}), ...(q ? { q } : {}) }
}
