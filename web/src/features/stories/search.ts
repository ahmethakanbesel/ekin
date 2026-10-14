import { STORY_TABS, type StoryTab } from "./model"

export type StoriesSearch = { tab?: StoryTab }

/** `?tab=progress` — the default tab (review) and unknown values are dropped. */
export function validateStoriesSearch(search: Record<string, unknown>): StoriesSearch {
  const tab = STORY_TABS.find((t) => t === search.tab)
  return tab && tab !== "review" ? { tab } : {}
}
