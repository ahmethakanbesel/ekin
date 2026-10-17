import type { LibraryAssetKind } from "@/api/client"
import { parseSearchText } from "@/lib/utils"

export const LIBRARY_TABS = ["all", "music", "video", "photo"] as const
export type LibraryTab = (typeof LIBRARY_TABS)[number]

export type LibrarySearch = {
  kind?: LibraryAssetKind
  q?: string
  archived?: true
  /** The asset open in the sheet. */
  asset?: string
}

/** `?kind=music&q=calm&archived=1&asset=<id>` — anything else is dropped. */
export function validateLibrarySearch(
  search: Record<string, unknown>
): LibrarySearch {
  const out: LibrarySearch = {}
  if (
    search.kind === "music" ||
    search.kind === "video" ||
    search.kind === "photo"
  )
    out.kind = search.kind
  const q = parseSearchText(search.q)
  if (q) out.q = q
  if (
    search.archived === true ||
    search.archived === "1" ||
    search.archived === 1
  )
    out.archived = true
  if (typeof search.asset === "string" && /^[0-9a-f-]{36}$/i.test(search.asset))
    out.asset = search.asset
  return out
}
