import {
  infiniteQueryOptions,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query"

import { api, call, type Job, type JobStatus } from "@/api/client"
import { keys } from "./keys"
import { cursorPaging, pageRows, type Cursor } from "./paging"

/** Requests (traces) per page, not jobs. */
export const JOB_PAGE_SIZE = 50

type JobPage = { jobs: Job[]; nextCursor?: string }
export type JobPages = InfiniteData<JobPage, Cursor>

export const jobRows = (data: JobPages) => pageRows(data.pages, (p) => p.jobs)

/**
 * Every job of the newest requests, a page of requests at a time. A status
 * or kind keeps the requests with a job of that status and kind.
 */
export const jobsQuery = (
  workspaceId: string,
  { status, kind = [] }: { status: JobStatus[]; kind?: string[] }
) =>
  infiniteQueryOptions({
    queryKey: keys.jobList(workspaceId, { status, kind, limit: JOB_PAGE_SIZE }),
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/jobs", {
          params: {
            path: { workspaceId },
            query: {
              status: status.length > 0 ? status : undefined,
              kind: kind.length > 0 ? kind : undefined,
              limit: JOB_PAGE_SIZE,
              cursor: pageParam,
            },
          },
          signal,
        }),
      ),
    ...cursorPaging,
    select: jobRows,
  })

export function useCancelJob(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (jobId: string) =>
      call(
        api.POST("/workspaces/{workspaceId}/jobs/{jobId}/cancel", {
          params: { path: { workspaceId, jobId } },
        }),
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.jobs(workspaceId) })
    },
  })
}
