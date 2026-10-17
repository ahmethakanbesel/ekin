import { infiniteQueryOptions, type InfiniteData } from "@tanstack/react-query"

import { api, call, type Notification } from "@/api/client"
import { keys } from "./keys"
import { cursorPaging, pageRows, type Cursor } from "./paging"

/** Notifications per page of the bell's list. */
export const NOTIFICATION_PAGE_SIZE = 30
const POLL_MS = 60_000

type NotificationPage = { notifications: Notification[]; nextCursor?: string }

const notificationRows = (data: InfiniteData<NotificationPage, Cursor>) =>
  pageRows(data.pages, (p) => p.notifications)

/**
 * The workspace's notifications, newest first, a page at a time. Polling
 * refetches the pages loaded so far, so new ones arrive on the first.
 */
export const notificationsQuery = (workspaceId: string) =>
  infiniteQueryOptions({
    queryKey: keys.notificationList(workspaceId, { limit: NOTIFICATION_PAGE_SIZE }),
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/notifications", {
          params: {
            path: { workspaceId },
            query: { limit: NOTIFICATION_PAGE_SIZE, cursor: pageParam },
          },
          signal,
        })
      ),
    ...cursorPaging,
    select: notificationRows,
    refetchInterval: POLL_MS,
  })
