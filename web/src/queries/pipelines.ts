import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type Pipeline, type PipelineDefinition } from "@/api/client"
import { keys } from "./keys"

export const pipelinesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.pipelines(workspaceId),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/pipelines", {
          params: { path: { workspaceId } },
          signal,
        }),
      )
      return res.pipelines
    },
  })

export const pipelineQuery = (workspaceId: string, pipelineId: string) =>
  queryOptions({
    queryKey: keys.pipeline(workspaceId, pipelineId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/pipelines/{pipelineId}", {
          params: { path: { workspaceId, pipelineId } },
          signal,
        }),
      ),
  })

/** Writes a changed pipeline into the detail cache and refreshes the list. */
function useStorePipeline(workspaceId: string) {
  const qc = useQueryClient()
  return (p: Pipeline) => {
    qc.setQueryData(keys.pipeline(workspaceId, p.id), p)
    qc.setQueryData<Pipeline[]>(keys.pipelines(workspaceId), (old) =>
      old?.map((x) => (x.id === p.id ? p : x)),
    )
    void qc.invalidateQueries({ queryKey: keys.pipelines(workspaceId), exact: true })
  }
}

export function useCreatePipeline(workspaceId: string) {
  const store = useStorePipeline(workspaceId)
  return useMutation({
    mutationFn: (body: PipelineDefinition) =>
      call(
        api.POST("/workspaces/{workspaceId}/pipelines", {
          params: { path: { workspaceId } },
          body,
        }),
      ),
    onSuccess: store,
  })
}

export function useRedefinePipeline(workspaceId: string, pipelineId: string) {
  const store = useStorePipeline(workspaceId)
  return useMutation({
    mutationFn: (body: { version: number; definition: PipelineDefinition }) =>
      call(
        api.PUT("/workspaces/{workspaceId}/pipelines/{pipelineId}", {
          params: { path: { workspaceId, pipelineId } },
          body,
        }),
      ),
    onSuccess: store,
  })
}

export function usePausePipeline(workspaceId: string) {
  const store = useStorePipeline(workspaceId)
  return useMutation({
    mutationFn: (pipelineId: string) =>
      call(
        api.POST("/workspaces/{workspaceId}/pipelines/{pipelineId}/pause", {
          params: { path: { workspaceId, pipelineId } },
        }),
      ),
    onSuccess: store,
  })
}

export function useResumePipeline(workspaceId: string) {
  const store = useStorePipeline(workspaceId)
  return useMutation({
    mutationFn: (pipelineId: string) =>
      call(
        api.POST("/workspaces/{workspaceId}/pipelines/{pipelineId}/resume", {
          params: { path: { workspaceId, pipelineId } },
        }),
      ),
    onSuccess: store,
  })
}

export function useDeletePipeline(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (pipelineId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/pipelines/{pipelineId}", {
          params: { path: { workspaceId, pipelineId } },
        }),
      )
      return pipelineId
    },
    onSuccess: (pipelineId) => {
      qc.setQueryData<Pipeline[]>(keys.pipelines(workspaceId), (old) =>
        old?.filter((p) => p.id !== pipelineId),
      )
      qc.removeQueries({ queryKey: keys.pipeline(workspaceId, pipelineId) })
      void qc.invalidateQueries({ queryKey: keys.pipelines(workspaceId), exact: true })
    },
  })
}

export function useRunPipeline(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ pipelineId, count }: { pipelineId: string; count: number }) =>
      call(
        api.POST("/workspaces/{workspaceId}/pipelines/{pipelineId}/runs", {
          params: { path: { workspaceId, pipelineId } },
          body: { count },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.jobs(workspaceId) })
      void qc.invalidateQueries({ queryKey: keys.pipelines(workspaceId) })
    },
  })
}
