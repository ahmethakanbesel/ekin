import { queryOptions, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query"

import {
  API_BASE,
  api,
  call,
  toApiError,
  ApiError,
  isApiError,
  type AdHocInput,
  type Deliverable,
  type DeliverableEdit,
  type DeliverableStatus,
  type FeedbackInput,
  type Story,
} from "@/api/client"
import { keys } from "./keys"

export const STORY_LIST_LIMIT = 200

/** Statuses in which the system (not a person) moves a deliverable on. */
export const WORKING: readonly DeliverableStatus[] = [
  "queued",
  "drafting",
  "rendering",
  "finalizing",
  "publishing",
]

export const isWorking = (d: Pick<Deliverable, "status">) => WORKING.includes(d.status)

/** Whether any deliverable of the story is being worked on. */
export const storyWorking = (s: Story) => s.deliverables.some(isWorking)

const POLL_LIST_MS = 5_000
const POLL_STORY_MS = 3_000

export const storiesQuery = (workspaceId: string, status: DeliverableStatus[]) =>
  queryOptions({
    queryKey: keys.storyList(workspaceId, { status, limit: STORY_LIST_LIMIT }),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/stories", {
          params: {
            path: { workspaceId },
            query: {
              status: status.length > 0 ? status : undefined,
              limit: STORY_LIST_LIMIT,
            },
          },
          signal,
        })
      )
      return res.stories
    },
    refetchInterval: (q) => (q.state.data?.some(storyWorking) ? POLL_LIST_MS : false),
  })

export const storyQuery = (workspaceId: string, storyId: string) =>
  queryOptions({
    queryKey: keys.story(workspaceId, storyId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/stories/{storyId}", {
          params: { path: { workspaceId, storyId } },
          signal,
        })
      ),
    refetchInterval: (q) => (q.state.data && storyWorking(q.state.data) ? POLL_STORY_MS : false),
  })

/**
 * A deliverable's revisions, oldest first. Keyed by the deliverable's
 * revision and status as well, so a new revision, or a render adding a
 * preview to the latest one, fetches the list again.
 */
export const revisionsQuery = (workspaceId: string, storyId: string, d: Deliverable) =>
  queryOptions({
    queryKey: [
      ...keys.revisions(workspaceId, storyId, d.id),
      { revision: d.revision, status: d.status },
    ] as const,
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/revisions", {
          params: { path: { workspaceId, storyId, deliverableId: d.id } },
          signal,
        })
      )
      return res.revisions
    },
  })

/** Where a revision's artifact is served (same origin, session cookie, Range support). */
export function artifactUrl(
  workspaceId: string,
  storyId: string,
  deliverableId: string,
  revision: number,
  name: string,
  download = false
): string {
  const enc = encodeURIComponent
  const url = `${API_BASE}/workspaces/${enc(workspaceId)}/stories/${enc(storyId)}/deliverables/${enc(deliverableId)}/revisions/${revision}/${enc(name)}`
  return download ? `${url}?download=1` : url
}

/**
 * A text artifact (script.md, article.md, metadata.json) of a revision.
 * What the agent wrote for a revision does not change afterwards.
 */
export const artifactTextQuery = (
  workspaceId: string,
  storyId: string,
  deliverableId: string,
  revision: number,
  name: string
) =>
  queryOptions({
    queryKey: keys.artifact(workspaceId, storyId, deliverableId, revision, name),
    queryFn: async ({ signal }) => {
      let res: Response
      try {
        res = await fetch(artifactUrl(workspaceId, storyId, deliverableId, revision, name), {
          signal,
          credentials: "same-origin",
        })
      } catch (cause) {
        if (signal.aborted) throw cause
        throw new ApiError(0, "NetworkError", cause instanceof Error ? cause.message : String(cause))
      }
      const text = await res.text()
      if (!res.ok) {
        let body: unknown = text
        try {
          body = JSON.parse(text)
        } catch {
          // not JSON; keep the text
        }
        throw toApiError(res.status, body, res.statusText)
      }
      return text
    },
    staleTime: Infinity,
  })

/** Writes a changed story into the detail cache and refreshes the lists. */
function useStoreStory(workspaceId: string) {
  const qc = useQueryClient()
  return (s: Story) => {
    qc.setQueryData(keys.story(workspaceId, s.id), s)
    void qc.invalidateQueries({ queryKey: keys.storyLists(workspaceId) })
  }
}

export type DeliverableAction = "approve" | "reject" | "retry" | "publish"

function postAction(
  action: DeliverableAction,
  path: { workspaceId: string; storyId: string; deliverableId: string }
) {
  const params = { path }
  switch (action) {
    case "approve":
      return api.POST(
        "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/approve",
        { params }
      )
    case "reject":
      return api.POST(
        "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/reject",
        { params }
      )
    case "retry":
      return api.POST(
        "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/retry",
        { params }
      )
    case "publish":
      return api.POST(
        "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/publish",
        { params }
      )
  }
}

/**
 * Approves a deliverable outside a component (a delayed approval outlives
 * the rail that started it) and stores the story it returns. keepalive lets
 * the request finish while the page unloads.
 */
export async function approveDeliverable(
  qc: QueryClient,
  path: { workspaceId: string; storyId: string; deliverableId: string },
  keepalive = false
): Promise<Story> {
  const s = await call(
    api.POST("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/approve", {
      params: { path },
      keepalive,
    })
  )
  qc.setQueryData(keys.story(path.workspaceId, s.id), s)
  void qc.invalidateQueries({ queryKey: keys.storyLists(path.workspaceId) })
  return s
}

/** Approve, reject, retry or publish one deliverable. */
export function useDeliverableAction(workspaceId: string, storyId: string, action: DeliverableAction) {
  const store = useStoreStory(workspaceId)
  return useMutation({
    mutationFn: (deliverableId: string) =>
      call(postAction(action, { workspaceId, storyId, deliverableId })),
    onSuccess: store,
  })
}

export function useRequestChanges(workspaceId: string, storyId: string) {
  const store = useStoreStory(workspaceId)
  return useMutation({
    mutationFn: ({ deliverableId, body }: { deliverableId: string; body: FeedbackInput }) =>
      call(
        api.POST("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/changes", {
          params: { path: { workspaceId, storyId, deliverableId } },
          body,
        })
      ),
    onSuccess: store,
  })
}

/**
 * The editable text of a deliverable's latest revision. Fetched afresh
 * each time the editor opens and never refetched behind it, so a window
 * refocus cannot swap the text being edited.
 */
export const deliverableContentQuery = (workspaceId: string, storyId: string, deliverableId: string) =>
  queryOptions({
    queryKey: keys.content(workspaceId, storyId, deliverableId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/content", {
          params: { path: { workspaceId, storyId, deliverableId } },
          signal,
        })
      ),
    staleTime: Infinity,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  })

/**
 * Saves a reviewer's edit as a new revision. The server answers 409 when
 * `revision` is no longer the latest; the story is refetched then, so the
 * page shows what changed.
 */
export function useEditDeliverableContent(workspaceId: string, storyId: string, deliverableId: string) {
  const qc = useQueryClient()
  const store = useStoreStory(workspaceId)
  return useMutation({
    mutationFn: (body: DeliverableEdit) =>
      call(
        api.PUT("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/content", {
          params: { path: { workspaceId, storyId, deliverableId } },
          body,
        })
      ),
    onSuccess: (story) => {
      store(story)
      void qc.invalidateQueries({ queryKey: keys.revisions(workspaceId, storyId, deliverableId) })
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 409) {
        void qc.invalidateQueries({ queryKey: keys.story(workspaceId, storyId), exact: true })
        void qc.invalidateQueries({ queryKey: keys.revisions(workspaceId, storyId, deliverableId) })
      }
    },
  })
}

/** Deletes a story. While a step runs on it the server answers 409 Conflict. */
export function useDeleteStory(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (storyId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/stories/{storyId}", {
          params: { path: { workspaceId, storyId } },
        })
      )
      return storyId
    },
    onSuccess: (storyId) => {
      qc.setQueriesData<Story[]>({ queryKey: keys.storyLists(workspaceId) }, (old) =>
        old?.filter((s) => s.id !== storyId)
      )
      void qc.invalidateQueries({ queryKey: keys.storyLists(workspaceId) })
    },
  })
}

/** Queues an ad-hoc request; the stories appear once planning finishes. */
export function useCreateStories(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: AdHocInput) =>
      call(
        api.POST("/workspaces/{workspaceId}/stories", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: keys.jobs(workspaceId) })
      void qc.invalidateQueries({ queryKey: keys.storyLists(workspaceId) })
    },
  })
}
