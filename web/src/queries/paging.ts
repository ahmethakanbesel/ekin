import { hashKey, type QueryKey } from "@tanstack/react-query"

/**
 * The page parameter of a keyset-paged list: the `nextCursor` the
 * previous page returned, none for the first. A cursor only continues the
 * listing it came from, so every filter is part of the query key.
 */
export type Cursor = string | undefined

/** The first page has no cursor; the last page returns none. */
export const cursorPaging = {
  initialPageParam: undefined as Cursor,
  getNextPageParam: (last: { nextCursor?: string }): Cursor => last.nextCursor,
}

/** Drops the search from a key's filter, to compare the rest. */
const withoutSearch = (key: QueryKey): QueryKey =>
  key.map((part) =>
    part !== null && typeof part === "object" && !Array.isArray(part) && "q" in part
      ? { ...part, q: undefined }
      : part
  )

/**
 * placeholderData for a paged list: while only its search changed, the
 * rows already shown stay up, so typing does not flash a skeleton. Any
 * other change (a tab, a status) loads afresh.
 */
export function keepWhileSearching<T>(queryKey: QueryKey) {
  return (prev: T | undefined, prevQuery: { queryKey: QueryKey } | undefined): T | undefined =>
    prevQuery && hashKey(withoutSearch(prevQuery.queryKey)) === hashKey(withoutSearch(queryKey))
      ? prev
      : undefined
}

/**
 * The rows of every loaded page, each once. A refetch reloads the pages
 * one after another, so a row that moved meanwhile (a request with a new
 * job, a keyword that changed status) could otherwise show twice.
 */
export function pageRows<P, T extends { id: string }>(pages: P[], rows: (page: P) => T[]): T[] {
  const seen = new Set<string>()
  const out: T[] = []
  for (const page of pages)
    for (const row of rows(page)) {
      if (seen.has(row.id)) continue
      seen.add(row.id)
      out.push(row)
    }
  return out
}
