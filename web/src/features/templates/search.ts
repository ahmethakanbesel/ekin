import { TEMPLATE_TABS, type TemplateTab } from "./model"

export type TemplatesSearch = { kind?: Exclude<TemplateTab, "all"> }

/** `?kind=video` — the default tab (all) and unknown values are dropped. */
export function validateTemplatesSearch(search: Record<string, unknown>): TemplatesSearch {
  const kind = TEMPLATE_TABS.find((t) => t === search.kind)
  return kind && kind !== "all" ? { kind } : {}
}
