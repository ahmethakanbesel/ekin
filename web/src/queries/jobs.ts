import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { api, call, type JobStatus } from "@/api/client"
import { keys } from "./keys"

/** Requests (traces), not jobs. */
export const JOB_LIST_LIMIT = 50

export const jobsQuery = (workspaceId: string, status: JobStatus[]) =>
  queryOptions({
    queryKey: keys.jobList(workspaceId, { status, limit: JOB_LIST_LIMIT }),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/jobs", {
          params: {
            path: { workspaceId },
            query: {
              status: status.length > 0 ? status : undefined,
              limit: JOB_LIST_LIMIT,
            },
          },
          signal,
        }),
      )
      return res.jobs
    },
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
