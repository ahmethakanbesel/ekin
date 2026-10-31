import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call } from "@/api/client"
import { keys } from "./keys"

export const searchConsoleQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.searchConsole(workspaceId),
    queryFn: ({ signal }) =>
      call(api.GET("/workspaces/{workspaceId}/integrations/searchconsole", { params: { path: { workspaceId } }, signal })),
  })

/** The properties the connected account can read (asks Google). */
export const searchPropertiesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.searchProperties(workspaceId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/integrations/searchconsole/properties", { params: { path: { workspaceId } }, signal })
      ),
    staleTime: 60_000,
  })

export const searchOverviewQuery = (workspaceId: string, days: number) =>
  queryOptions({
    queryKey: keys.searchOverview(workspaceId, days),
    queryFn: ({ signal }) =>
      call(api.GET("/workspaces/{workspaceId}/search", { params: { path: { workspaceId }, query: { days } }, signal })),
  })

export const storySearchQuery = (workspaceId: string, storyId: string) =>
  queryOptions({
    queryKey: keys.storySearch(workspaceId, storyId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/stories/{storyId}/search", { params: { path: { workspaceId, storyId } }, signal })
      ),
  })

function useRefresh(workspaceId: string) {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: keys.searchConsole(workspaceId) })
    void qc.invalidateQueries({ queryKey: ["workspaces", workspaceId, "search"] })
  }
}

/** Asks for Google's consent URL; the caller sends the browser there. */
export function useConnectSearchConsole(workspaceId: string) {
  return useMutation({
    mutationFn: async () =>
      (await call(api.POST("/workspaces/{workspaceId}/integrations/searchconsole", { params: { path: { workspaceId } } }))).url,
  })
}

export function useChooseSearchProperty(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: (property: string) =>
      call(
        api.PUT("/workspaces/{workspaceId}/integrations/searchconsole", { params: { path: { workspaceId } }, body: { property } })
      ),
    onSuccess: refresh,
  })
}

export function useSyncSearchConsole(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: () =>
      call(api.POST("/workspaces/{workspaceId}/integrations/searchconsole/sync", { params: { path: { workspaceId } } })),
    onSuccess: refresh,
  })
}

export function useDisconnectSearchConsole(workspaceId: string) {
  const refresh = useRefresh(workspaceId)
  return useMutation({
    mutationFn: () =>
      call(api.DELETE("/workspaces/{workspaceId}/integrations/searchconsole", { params: { path: { workspaceId } } })),
    onSuccess: refresh,
  })
}
