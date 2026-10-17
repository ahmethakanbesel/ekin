import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type WorkspaceProductionInput } from "@/api/client"
import { keys } from "./keys"

/** The workspace's production defaults and what the server gives under them. */
export const workspaceProductionQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.production(workspaceId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/production", { params: { path: { workspaceId } }, signal })
      ),
  })

export function useSaveWorkspaceProduction(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: WorkspaceProductionInput) =>
      call(api.PUT("/workspaces/{workspaceId}/production", { params: { path: { workspaceId } }, body })),
    onSuccess: (saved) => qc.setQueryData(keys.production(workspaceId), saved),
  })
}
