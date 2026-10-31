import type { SiteFindingStatus, SitePageClass } from "@/api/client"
import { parseSearchText } from "@/lib/utils"

export const SITE_VIEWS = ["findings", "pages"] as const
export type SiteView = (typeof SITE_VIEWS)[number]
export const FINDING_TABS: SiteFindingStatus[] = ["open", "ignored", "resolved"]
export const PAGE_CLASSES = ["all", "ok", "redirect", "error"] as const
export type PageClassTab = SitePageClass | "all"

export type SiteSearch = {
  view?: "pages"
  status?: Exclude<SiteFindingStatus, "open">
  class?: Exclude<PageClassTab, "all">
  q?: string
}

/** `?view=pages&class=error&q=blog` — defaults (findings, open, all) and unknown values are dropped. */
export function validateSiteSearch(
  search: Record<string, unknown>
): SiteSearch {
  const view = search.view === "pages" ? "pages" : undefined
  const status = FINDING_TABS.find(
    (s) => s !== "open" && s === search.status
  ) as SiteSearch["status"]
  const cls = PAGE_CLASSES.find(
    (c) => c !== "all" && c === search.class
  ) as SiteSearch["class"]
  const q = parseSearchText(search.q)
  return {
    ...(view ? { view } : {}),
    ...(status ? { status } : {}),
    ...(cls ? { class: cls } : {}),
    ...(q ? { q } : {}),
  }
}
