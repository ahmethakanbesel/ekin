import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import {
  api,
  call,
  type OpportunityInput,
  type OpportunityStatus,
  type SEOProfile,
} from "@/api/client"
import { keys } from "./keys"

/** The server takes at most this many keywords per import request. */
export const IMPORT_BATCH = 500

export const seoProfileQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.seoProfile(workspaceId),
    queryFn: ({ signal }) =>
      call(api.GET("/workspaces/{workspaceId}/seo", { params: { path: { workspaceId } }, signal })),
  })

export function useSaveSeoProfile(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: SEOProfile) =>
      call(api.PUT("/workspaces/{workspaceId}/seo", { params: { path: { workspaceId } }, body })),
    onSuccess: (data) => qc.setQueryData(keys.seoProfile(workspaceId), data),
  })
}

export const opportunitiesQuery = (workspaceId: string, status: OpportunityStatus | "all") =>
  queryOptions({
    queryKey: keys.opportunityList(workspaceId, status),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/opportunities", {
          params: {
            path: { workspaceId },
            query: { status: status === "all" ? undefined : status },
          },
          signal,
        })
      )
      return res.opportunities
    },
  })

/** Adds keywords in batches the server accepts; resolves to how many were new. */
export function useImportOpportunities(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (items: OpportunityInput[]) => {
      let added = 0
      for (let i = 0; i < items.length; i += IMPORT_BATCH) {
        const res = await call(
          api.POST("/workspaces/{workspaceId}/opportunities", {
            params: { path: { workspaceId } },
            body: { items: items.slice(i, i + IMPORT_BATCH) },
          })
        )
        added += res.added
      }
      return added
    },
    // Also after a failure part-way: earlier batches may have landed.
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: keys.opportunities(workspaceId) })
    },
  })
}

export function useSetOpportunityStatus(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: "open" | "dismissed" }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/opportunities/{opportunityId}", {
          params: { path: { workspaceId, opportunityId: id } },
          body: { status },
        })
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.opportunities(workspaceId) })
    },
  })
}

export function useRefreshOpportunities(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (count: number) =>
      call(
        api.POST("/workspaces/{workspaceId}/opportunities/refresh", {
          params: { path: { workspaceId } },
          body: { count },
        })
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.jobs(workspaceId) })
    },
  })
}
