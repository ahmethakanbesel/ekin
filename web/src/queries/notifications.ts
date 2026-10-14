import { queryOptions } from "@tanstack/react-query"

import { api, call } from "@/api/client"
import { keys } from "./keys"

export const NOTIFICATION_LIMIT = 30
const POLL_MS = 60_000

export const notificationsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.notifications(workspaceId),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/notifications", {
          params: { path: { workspaceId }, query: { limit: NOTIFICATION_LIMIT } },
          signal,
        })
      )
      return res.notifications
    },
    refetchInterval: POLL_MS,
  })
