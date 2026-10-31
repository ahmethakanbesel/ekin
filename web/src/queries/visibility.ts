import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type VisibilityOverview, type VisibilitySettings } from "@/api/client"
import { keys } from "./keys"

/** How often the page refreshes while a check runs or prompts are drafted. */
const POLL_MS = 4_000

export const runActive = (o: VisibilityOverview | undefined) =>
  !!o?.runs[0] && (o.runs[0].status === "queued" || o.runs[0].status === "running")

export const visibilityQuery = (workspaceId: string, polling = false) =>
  queryOptions({
    queryKey: keys.visibility(workspaceId),
    queryFn: ({ signal }) => call(api.GET("/workspaces/{workspaceId}/visibility", { params: { path: { workspaceId } }, signal })),
    refetchInterval: (q) => (polling || runActive(q.state.data) ? POLL_MS : false),
  })

/**
 * The latest finished run's report (runId ""), or a given run's. `version`
 * names what the latest is now (the newest run and its status), so a
 * check that finishes reloads it.
 */
export const visibilityReportQuery = (workspaceId: string, runId: string, version = "") =>
  queryOptions({
    queryKey: [...keys.visibilityReport(workspaceId, runId), version],
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/visibility/report", {
          params: { path: { workspaceId }, query: { runId: runId || undefined } },
          signal,
        })
      ),
  })

function useRefresh(workspaceId: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: keys.visibility(workspaceId) })
}

export function useSaveVisibility(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: VisibilitySettings) =>
      call(api.PUT("/workspaces/{workspaceId}/visibility", { params: { path: { workspaceId } }, body })),
    onSuccess: (data) => qc.setQueryData(keys.visibility(workspaceId), data),
  })
}

export function useAddPrompt(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: (text: string) =>
      call(api.POST("/workspaces/{workspaceId}/visibility/prompts", { params: { path: { workspaceId } }, body: { text } })),
    onSuccess: refresh,
  })
}

export function useUpdatePrompt(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; text?: string; active?: boolean }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/visibility/prompts/{promptId}", {
          params: { path: { workspaceId, promptId: id } },
          body,
        })
      ),
    onSuccess: refresh,
  })
}

export function useDeletePrompt(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: (id: string) =>
      call(
        api.DELETE("/workspaces/{workspaceId}/visibility/prompts/{promptId}", { params: { path: { workspaceId, promptId: id } } })
      ),
    onSuccess: refresh,
  })
}

export function useDraftPrompts(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: (count: number) =>
      call(api.POST("/workspaces/{workspaceId}/visibility/prompts/draft", { params: { path: { workspaceId } }, body: { count } })),
    onSuccess: refresh,
  })
}

export function useStartRun(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: () => call(api.POST("/workspaces/{workspaceId}/visibility/runs", { params: { path: { workspaceId } } })),
    onSuccess: refresh,
  })
}
