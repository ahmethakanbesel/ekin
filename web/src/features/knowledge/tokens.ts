import { useQueries } from "@tanstack/react-query"

import type { Document, DocumentPage } from "@/api/client"
import { m } from "@/i18n"
import { formatCompact } from "@/lib/format"
import { chunkOf, documentPagesQuery } from "@/queries/documents"

/** A page range of a document; no from/to means the whole document. */
export type RangeRequest = {
  doc: Document | undefined
  from?: number
  to?: number
}

export type TokenEstimate = {
  from: number
  to: number
  pages: number
  tokens: number
  /** A proportional guess (the range's share of the document's tokens). */
  approximate: boolean
  whole: boolean
}

/**
 * Ranges spanning more chunks than this are estimated proportionally
 * instead of downloading every page's text just to add up its tokens.
 */
const MAX_EXACT_CHUNKS = 5

type Plan =
  | { kind: "none" }
  | { kind: "whole"; doc: Document }
  | { kind: "approx"; doc: Document; from: number; to: number }
  | { kind: "exact"; doc: Document; from: number; to: number; chunks: number[] }

function plan(r: RangeRequest): Plan {
  const doc = r.doc
  if (!doc || doc.status !== "ready") return { kind: "none" }
  if (r.from === undefined && r.to === undefined) return { kind: "whole", doc }
  const { from, to } = r
  if (from === undefined || to === undefined || from < 1 || to < from || to > doc.pageCount) {
    return { kind: "none" }
  }
  if (from === 1 && to === doc.pageCount) return { kind: "whole", doc }
  const first = chunkOf(from)
  const last = chunkOf(to)
  if (last - first + 1 > MAX_EXACT_CHUNKS) return { kind: "approx", doc, from, to }
  const chunks = Array.from({ length: last - first + 1 }, (_, i) => first + i)
  return { kind: "exact", doc, from, to, chunks }
}

function proportional(doc: Document, from: number, to: number): TokenEstimate {
  const pages = to - from + 1
  const tokens = doc.pageCount > 0 ? Math.round((doc.tokens * pages) / doc.pageCount) : 0
  return { from, to, pages, tokens, approximate: true, whole: false }
}

/**
 * Token estimates for page ranges, one per request (undefined when the
 * document is unknown, not ready or the range is invalid). Short ranges
 * add up the per-page counts, fetching the chunks they need (shared with
 * the viewer's cache); until those arrive, and for long ranges, the
 * estimate is proportional and marked approximate.
 */
export function useTokenEstimates(
  workspaceId: string,
  requests: RangeRequest[],
): Array<TokenEstimate | undefined> {
  const plans = requests.map(plan)
  const wanted = new Map<string, { documentId: string; chunk: number }>()
  for (const p of plans) {
    if (p.kind !== "exact") continue
    for (const chunk of p.chunks) wanted.set(`${p.doc.id}:${chunk}`, { documentId: p.doc.id, chunk })
  }
  const list = [...wanted.entries()]
  const results = useQueries({
    queries: list.map(([, w]) => documentPagesQuery(workspaceId, w.documentId, w.chunk)),
  })
  const loaded = new Map<string, DocumentPage[] | undefined>(
    list.map(([key], i) => [key, results[i]?.data]),
  )

  return plans.map((p) => {
    switch (p.kind) {
      case "none":
        return undefined
      case "whole":
        return {
          from: 1,
          to: p.doc.pageCount,
          pages: p.doc.pageCount,
          tokens: p.doc.tokens,
          approximate: false,
          whole: true,
        }
      case "approx":
        return proportional(p.doc, p.from, p.to)
      case "exact": {
        let tokens = 0
        for (const chunk of p.chunks) {
          const pages = loaded.get(`${p.doc.id}:${chunk}`)
          if (!pages) return proportional(p.doc, p.from, p.to)
          for (const page of pages) {
            if (page.number >= p.from && page.number <= p.to) tokens += page.tokens
          }
        }
        return { from: p.from, to: p.to, pages: p.to - p.from + 1, tokens, approximate: false, whole: false }
      }
    }
  })
}

/** "12.4k tokens", or "≈ 12.4k tokens" for a proportional guess. */
export function tokensLabel(tokens: number, approximate = false): string {
  const n = formatCompact(tokens)
  return approximate ? m.knowledge.approxTokens(n) : m.knowledge.tokens(n)
}
