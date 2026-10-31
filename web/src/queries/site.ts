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
  type SiteFinding,
  type SiteFindingStatus,
  type SiteOverview,
  type SitePage,
  type SitePageClass,
  type SiteSettings,
} from "@/api/client"
import { keys } from "./keys"
import { cursorPaging, keepWhileSearching, type Cursor } from "./paging"

/** How often the overview refreshes while a crawl is queued or running. */
const POLL_CRAWL_MS = 3_000

export const crawlActive = (o: SiteOverview | undefined) =>
  o?.latestCrawl?.status === "queued" || o?.latestCrawl?.status === "running"

/** The site, its latest crawls and its open findings per rule. */
export const siteQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.site(workspaceId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/site", {
          params: { path: { workspaceId } },
          signal,
        })
      ),
    refetchInterval: (q) => (crawlActive(q.state.data) ? POLL_CRAWL_MS : false),
  })

export function useSaveSite(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: SiteSettings) =>
      call(
        api.PUT("/workspaces/{workspaceId}/site", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: (data) => {
      qc.setQueryData(keys.site(workspaceId), data)
      void qc.invalidateQueries({ queryKey: keys.site(workspaceId) })
    },
  })
}

export function useCrawlSite(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      call(
        api.POST("/workspaces/{workspaceId}/site/crawls", {
          params: { path: { workspaceId } },
        })
      ),
    onSettled: () =>
      void qc.invalidateQueries({
        queryKey: keys.site(workspaceId),
        exact: true,
      }),
  })
}

export const SITE_PAGE_SIZE = 50

type PagePages = InfiniteData<
  { pages: SitePage[]; nextCursor?: string },
  Cursor
>

/** The latest crawl's pages, by address; `crawl` is that crawl's id. */
export const sitePagesQuery = (
  workspaceId: string,
  {
    pageClass,
    q = "",
    crawl,
  }: { pageClass: SitePageClass | "all"; q?: string; crawl: string }
) => {
  const queryKey = keys.sitePages(workspaceId, {
    class: pageClass,
    q,
    crawl,
    limit: SITE_PAGE_SIZE,
  })
  return infiniteQueryOptions({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/site/pages", {
          params: {
            path: { workspaceId },
            query: {
              class: pageClass === "all" ? undefined : pageClass,
              q: q || undefined,
              limit: SITE_PAGE_SIZE,
              cursor: pageParam,
            },
          },
          signal,
        })
      ),
    ...cursorPaging,
    select: (data: PagePages) => data.pages.flatMap((p) => p.pages),
    placeholderData: keepWhileSearching<PagePages>(queryKey),
  })
}

type FindingPages = InfiniteData<
  { findings: SiteFinding[]; nextCursor?: string },
  Cursor
>

/** Findings with status (of one rule, when given), most severe first. */
export const siteFindingsQuery = (
  workspaceId: string,
  {
    status,
    rule,
    crawl,
  }: { status: SiteFindingStatus; rule?: string; crawl: string }
) =>
  infiniteQueryOptions({
    queryKey: keys.siteFindings(workspaceId, {
      status,
      rule,
      crawl,
      limit: SITE_PAGE_SIZE,
    }),
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/site/findings", {
          params: {
            path: { workspaceId },
            query: {
              status: [status],
              rule,
              limit: SITE_PAGE_SIZE,
              cursor: pageParam,
            },
          },
          signal,
        })
      ),
    ...cursorPaging,
    select: (data: FindingPages) => data.pages.flatMap((p) => p.findings),
  })

export function useSetFindingStatus(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: "open" | "ignored" }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/site/findings/{findingId}", {
          params: { path: { workspaceId, findingId: id } },
          body: { status },
        })
      ),
    onSuccess: () =>
      void qc.invalidateQueries({ queryKey: keys.site(workspaceId) }),
  })
}

/** Open findings of rule no change holds yet, up to max (POST /changes takes 25). */
export async function claimableFindings(workspaceId: string, rule: string, max = 25): Promise<SiteFinding[]> {
  const out = await call(
    api.GET("/workspaces/{workspaceId}/site/findings", {
      params: { path: { workspaceId }, query: { status: ["open"], rule, limit: 200 } },
    })
  )
  return out.findings.filter((f) => !f.claimedBy).slice(0, max)
}

/** Starts a change to the site: findings to fix and/or instructions. */
export function useRequestChange(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: { findingIds?: string[]; instructions?: string }) =>
      call(
        api.POST("/workspaces/{workspaceId}/changes", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.site(workspaceId) })
      void qc.invalidateQueries({ queryKey: keys.stories(workspaceId) })
    },
  })
}
