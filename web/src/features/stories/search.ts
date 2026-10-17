import { parseSearchText } from "@/lib/utils"
import { STORY_TABS, type StoryTab } from "./model"

export type StoriesSearch = { tab?: StoryTab; q?: string }

/** `?tab=progress&q=sınav` — the default tab (review), unknown values and an empty search are dropped. */
export function validateStoriesSearch(search: Record<string, unknown>): StoriesSearch {
  const tab = STORY_TABS.find((t) => t === search.tab)
  const q = parseSearchText(search.q)
  return { ...(tab && tab !== "review" ? { tab } : {}), ...(q ? { q } : {}) }
}
