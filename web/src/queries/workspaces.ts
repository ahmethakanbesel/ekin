import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type Schemas, type Workspace } from "@/api/client"
import { keys } from "./keys"

export const workspacesQuery = () =>
  queryOptions({
    queryKey: keys.workspaces(),
    queryFn: async ({ signal }) => {
      const res = await call(api.GET("/workspaces", { signal }))
      return res.workspaces
    },
  })

export const workspaceQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.workspace(workspaceId),
    queryFn: ({ signal }) =>
      call(api.GET("/workspaces/{workspaceId}", { params: { path: { workspaceId } }, signal })),
  })

export function useCreateWorkspace() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas["WorkspaceInput"]) => call(api.POST("/workspaces", { body })),
    onSuccess: (ws) => {
      qc.setQueryData(keys.workspace(ws.id), ws)
      qc.setQueryData<Workspace[]>(keys.workspaces(), (old) => (old ? [...old, ws] : [ws]))
      void qc.invalidateQueries({ queryKey: keys.workspaces(), exact: true })
    },
  })
}

export function useUpdateWorkspace(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Schemas["WorkspacePatch"]) =>
      call(
        api.PATCH("/workspaces/{workspaceId}", {
          params: { path: { workspaceId } },
          body,
        }),
      ),
    onSuccess: (ws) => {
      qc.setQueryData(keys.workspace(ws.id), ws)
      qc.setQueryData<Workspace[]>(keys.workspaces(), (old) =>
        old?.map((w) => (w.id === ws.id ? ws : w)),
      )
    },
  })
}
