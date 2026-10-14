import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import {
  api,
  call,
  type DeliverableKind,
  type Template,
  type TemplateInput,
} from "@/api/client"
import { keys } from "./keys"

/** Built-ins first, then the workspace's own (the server's order). */
export const templatesQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.templates(workspaceId),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/templates", {
          params: { path: { workspaceId } },
          signal,
        })
      )
      return res.templates
    },
    // Built-ins never change and workspace ones rarely do.
    staleTime: 60_000,
  })

export const templateQuery = (workspaceId: string, templateId: string) =>
  queryOptions({
    queryKey: keys.template(workspaceId, templateId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/templates/{templateId}", {
          params: { path: { workspaceId, templateId } },
          signal,
        })
      ),
  })

/** Writes a changed template into the detail cache and refreshes the list. */
function useStoreTemplate(workspaceId: string) {
  const qc = useQueryClient()
  return (t: Template) => {
    qc.setQueryData(keys.template(workspaceId, t.id), t)
    qc.setQueryData<Template[]>(keys.templates(workspaceId), (old) => {
      if (!old) return old
      return old.some((x) => x.id === t.id) ? old.map((x) => (x.id === t.id ? t : x)) : [...old, t]
    })
    void qc.invalidateQueries({ queryKey: keys.templates(workspaceId), exact: true })
  }
}

export function useCreateTemplate(workspaceId: string) {
  const store = useStoreTemplate(workspaceId)
  return useMutation({
    mutationFn: (body: TemplateInput) =>
      call(
        api.POST("/workspaces/{workspaceId}/templates", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: store,
  })
}

export function useUpdateTemplate(workspaceId: string, templateId: string) {
  const store = useStoreTemplate(workspaceId)
  return useMutation({
    mutationFn: (body: TemplateInput) =>
      call(
        api.PUT("/workspaces/{workspaceId}/templates/{templateId}", {
          params: { path: { workspaceId, templateId } },
          body,
        })
      ),
    onSuccess: store,
  })
}

export function useDuplicateTemplate(workspaceId: string) {
  const store = useStoreTemplate(workspaceId)
  return useMutation({
    mutationFn: (templateId: string) =>
      call(
        api.POST("/workspaces/{workspaceId}/templates/{templateId}/duplicate", {
          params: { path: { workspaceId, templateId } },
        })
      ),
    onSuccess: store,
  })
}

export function useDeleteTemplate(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (templateId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/templates/{templateId}", {
          params: { path: { workspaceId, templateId } },
        })
      )
      return templateId
    },
    onSuccess: (templateId) => {
      qc.setQueryData<Template[]>(keys.templates(workspaceId), (old) =>
        old?.filter((t) => t.id !== templateId)
      )
      qc.removeQueries({ queryKey: keys.template(workspaceId, templateId) })
      void qc.invalidateQueries({ queryKey: keys.templates(workspaceId), exact: true })
      // Pipelines that used it now run without one.
      void qc.invalidateQueries({ queryKey: keys.pipelines(workspaceId) })
    },
  })
}

export type GenerateTemplateInput = {
  channel: string
  notes?: string
  kinds?: DeliverableKind[]
}

/** Queues the agent job that drafts a template from a channel; resolves to the job. */
export function useGenerateTemplate(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: GenerateTemplateInput) =>
      call(
        api.POST("/workspaces/{workspaceId}/templates/generate", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.jobs(workspaceId) })
    },
  })
}

export const jobQuery = (workspaceId: string, jobId: string) =>
  queryOptions({
    queryKey: keys.job(workspaceId, jobId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/jobs/{jobId}", {
          params: { path: { workspaceId, jobId } },
          signal,
        })
      ),
  })
