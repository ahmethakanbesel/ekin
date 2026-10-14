export type DocumentView = "text" | "pdf"

export type DocumentSearch = { page?: number; view?: DocumentView }

/** `?page=12&view=pdf` — anything else is dropped. */
export function validateDocumentSearch(search: Record<string, unknown>): DocumentSearch {
  const out: DocumentSearch = {}
  const page = Number(search.page)
  if (Number.isInteger(page) && page >= 1) out.page = page
  if (search.view === "pdf") out.view = "pdf"
  return out
}
