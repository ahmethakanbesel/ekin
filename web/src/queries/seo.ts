import {
  infiniteQueryOptions,
  queryOptions,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query"

import {
  api,
  call,
  type Opportunity,
  type OpportunityInput,
  type OpportunityStatus,
  type SEOProfile,
} from "@/api/client"
import { keys } from "./keys"
import { cursorPaging, keepWhileSearching, pageRows, type Cursor } from "./paging"

/** The server takes at most this many keywords per import request. */
export const IMPORT_BATCH = 500

export const seoProfileQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.seoProfile(workspaceId),
    queryFn: ({ signal }) =>
      call(api.GET("/workspaces/{workspaceId}/seo", { params: { path: { workspaceId } }, signal })),
  })

/**
 * The instance's OpenSEO connection and its projects (owners and admins;
 * free, spends no credits).
 */
export const openSeoStatusQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.openSeo(workspaceId),
    queryFn: ({ signal }) =>
      call(api.GET("/workspaces/{workspaceId}/seo/openseo", { params: { path: { workspaceId } }, signal })),
    staleTime: 60_000,
  })

export function useSaveSeoProfile(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: SEOProfile) =>
      call(api.PUT("/workspaces/{workspaceId}/seo", { params: { path: { workspaceId } }, body })),
    onSuccess: (data) => qc.setQueryData(keys.seoProfile(workspaceId), data),
  })
}

/** Opportunities per page of the list. */
export const OPPORTUNITY_PAGE_SIZE = 50

type OpportunityPage = { opportunities: Opportunity[]; nextCursor?: string }
type OpportunityPages = InfiniteData<OpportunityPage, Cursor>

const opportunityRows = (data: OpportunityPages) => pageRows(data.pages, (p) => p.opportunities)

/**
 * Keyword opportunities (open, used, dismissed; best score first within
 * each), a page at a time; `q` searches keywords on the server.
 */
export const opportunitiesQuery = (
  workspaceId: string,
  { status, q = "" }: { status: OpportunityStatus | "all"; q?: string }
) => {
  const queryKey = keys.opportunityList(workspaceId, { status, q, limit: OPPORTUNITY_PAGE_SIZE })
  return infiniteQueryOptions({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/opportunities", {
          params: {
            path: { workspaceId },
            query: {
              status: status === "all" ? undefined : [status],
              q: q || undefined,
              limit: OPPORTUNITY_PAGE_SIZE,
              cursor: pageParam,
            },
          },
          signal,
        })
      ),
    ...cursorPaging,
    select: opportunityRows,
    placeholderData: keepWhileSearching<OpportunityPages>(queryKey),
  })
}

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
