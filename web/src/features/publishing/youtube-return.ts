import { useEffect } from "react"
import { useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import { m } from "@/i18n"
import { keys } from "@/queries/keys"

/**
 * Where Google's consent screen sends the browser back to:
 * `/w/{ws}/settings?youtube=connected`, or `?youtube=error&reason=…`
 * (on `/` when the server could not tell which workspace it was for).
 */
export type YouTubeReturnSearch = {
  youtube?: "connected" | "error"
  /** The same for Search Console's consent screen. */
  searchconsole?: "connected" | "error"
  reason?: string
}

const outcome = (v: unknown) => (v === "connected" || v === "error" ? v : undefined)

export function validateYouTubeReturn(search: Record<string, unknown>): YouTubeReturnSearch {
  const youtube = outcome(search.youtube)
  const searchconsole = outcome(search.searchconsole)
  if (!youtube && !searchconsole) return {}
  const out: YouTubeReturnSearch = youtube ? { youtube } : { searchconsole }
  if ((youtube ?? searchconsole) === "error" && typeof search.reason === "string" && search.reason) {
    out.reason = search.reason.slice(0, 300)
  }
  return out
}

/** Announces the outcome of the Search Console consent flow once. */
export function useSearchConsoleReturn(search: YouTubeReturnSearch, clear: () => void, workspaceId?: string) {
  const qc = useQueryClient()
  const { searchconsole, reason } = search
  useEffect(() => {
    if (!searchconsole) return
    if (searchconsole === "connected") {
      toast.success(m.searchConsole.returnConnected, { id: "searchconsole-return" })
      if (workspaceId) void qc.invalidateQueries({ queryKey: keys.searchConsole(workspaceId) })
    } else {
      toast.error(m.searchConsole.returnError, { id: "searchconsole-return", description: reason })
    }
    clear()
  }, [searchconsole, reason, clear, qc, workspaceId])
}

/** Announces the outcome of the YouTube consent flow once, then drops it from the URL. */
export function useYouTubeReturn(
  search: YouTubeReturnSearch,
  clear: () => void,
  workspaceId?: string
) {
  const qc = useQueryClient()
  const { youtube, reason } = search
  useEffect(() => {
    if (!youtube) return
    // A fixed id: a re-run (StrictMode, a quick re-render) updates the
    // same toast instead of stacking a second one.
    if (youtube === "connected") {
      toast.success(m.youtube.returnConnected, { id: "youtube-return" })
      if (workspaceId) void qc.invalidateQueries({ queryKey: keys.integrations(workspaceId) })
    } else {
      toast.error(m.youtube.returnError, { id: "youtube-return", description: reason })
    }
    clear()
  }, [youtube, reason, clear, qc, workspaceId])
}
