import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type BlogInput, type Integrations, type Privacy } from "@/api/client"
import { keys } from "./keys"

export const integrationsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.integrations(workspaceId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/integrations", {
          params: { path: { workspaceId } },
          signal,
        })
      ),
  })

/** Asks for Google's consent URL; the caller sends the browser there. */
export function useConnectYouTube(workspaceId: string) {
  return useMutation({
    mutationFn: async () => {
      const res = await call(
        api.POST("/workspaces/{workspaceId}/integrations/youtube", {
          params: { path: { workspaceId } },
        })
      )
      return res.url
    },
  })
}

export function useSetYouTubePrivacy(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (privacy: Privacy) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/integrations/youtube", {
          params: { path: { workspaceId } },
          body: { privacy },
        })
      ),
    onSuccess: (data) => qc.setQueryData(keys.integrations(workspaceId), data),
  })
}

export function useDisconnectYouTube(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      call(
        api.DELETE("/workspaces/{workspaceId}/integrations/youtube", {
          params: { path: { workspaceId } },
        })
      ),
    onSuccess: () => {
      qc.setQueryData<Integrations>(keys.integrations(workspaceId), (old) =>
        old ? { ...old, youtube: { configured: old.youtube.configured, connected: false } } : old
      )
      void qc.invalidateQueries({ queryKey: keys.integrations(workspaceId) })
    },
  })
}

export function useSaveBlog(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: BlogInput) =>
      call(
        api.PUT("/workspaces/{workspaceId}/integrations/blog", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: (data) => qc.setQueryData(keys.integrations(workspaceId), data),
  })
}

export function useDeleteBlog(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      call(
        api.DELETE("/workspaces/{workspaceId}/integrations/blog", {
          params: { path: { workspaceId } },
        })
      ),
    onSuccess: () => {
      qc.setQueryData<Integrations>(keys.integrations(workspaceId), (old) =>
        old ? { youtube: old.youtube } : old
      )
      void qc.invalidateQueries({ queryKey: keys.integrations(workspaceId) })
    },
  })
}
