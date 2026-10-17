import {
  MutationObserver,
  infiniteQueryOptions,
  queryOptions,
  useMutation,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query"

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
  type MomentInput,
  type MomentPatch,
  type Revision,
  type Story,
} from "@/api/client"
import { keys } from "./keys"
import { cursorPaging, keepWhileSearching, pageRows, type Cursor } from "./paging"

/** Stories per page of a list: whole rows of the two- and four-column grid. */
export const STORY_PAGE_SIZE = 48

/**
 * How many stories a window holds (see storyWindowQuery): enough for any
 * real review queue, small enough to poll.
 */
export const STORY_WINDOW = 200

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

type StoryPage = { stories: Story[]; nextCursor?: string }
type StoryPages = InfiniteData<StoryPage, Cursor>

const storyRows = (data: StoryPages) => pageRows(data.pages, (p) => p.stories)

export type StoryFilter = { status: DeliverableStatus[]; q?: string }

/**
 * A list page's stories, newest first, a page at a time (`fetchNextPage`
 * follows the cursor). The data is the loaded stories, flattened.
 */
export const storiesQuery = (workspaceId: string, { status, q = "" }: StoryFilter) => {
  const queryKey = keys.storyList(workspaceId, { status, q, limit: STORY_PAGE_SIZE })
  return infiniteQueryOptions({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/stories", {
          params: {
            path: { workspaceId },
            query: {
              status: status.length > 0 ? status : undefined,
              q: q || undefined,
              limit: STORY_PAGE_SIZE,
              cursor: pageParam,
            },
          },
          signal,
        })
      ),
    ...cursorPaging,
    select: storyRows,
    placeholderData: keepWhileSearching<StoryPages>(queryKey),
    refetchInterval: (query) =>
      query.state.data?.pages.some((p) => p.stories.some(storyWorking)) ? POLL_LIST_MS : false,
  })
}

/** The newest stories with some statuses; `more` when there are older ones. */
export type StoryWindow = { stories: Story[]; more: boolean }

/**
 * The newest STORY_WINDOW stories with these statuses, in one request:
 * what counts, the review queue and names read, rather than a list a
 * person pages through. `more` means the window is cut off, so a count
 * taken from it is a floor ("200+").
 */
export const storyWindowQuery = (workspaceId: string, status: DeliverableStatus[]) =>
  queryOptions({
    queryKey: keys.storyWindow(workspaceId, status),
    queryFn: async ({ signal }): Promise<StoryWindow> => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/stories", {
          params: {
            path: { workspaceId },
            query: {
              status: status.length > 0 ? status : undefined,
              limit: STORY_WINDOW,
            },
          },
          signal,
        })
      )
      return { stories: res.stories, more: res.nextCursor !== undefined }
    },
    refetchInterval: (q) => (q.state.data?.stories.some(storyWorking) ? POLL_LIST_MS : false),
  })

/** "12", or "200+" for a window cut off at its limit. */
export const windowCount = (w: StoryWindow, n = w.stories.length) =>
  w.more ? `${n}+` : String(n)

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
 * the request finish while the page unloads. It still runs through the
 * mutation cache, so a 401 signs the reviewer out and a 403 refreshes their
 * role like any other mutation.
 */
export function approveDeliverable(
  qc: QueryClient,
  path: { workspaceId: string; storyId: string; deliverableId: string },
  keepalive = false
): Promise<Story> {
  return new MutationObserver(qc, {
    mutationFn: () =>
      call(
        api.POST("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/approve", {
          params: { path },
          keepalive,
        })
      ),
    onSuccess: (s: Story) => {
      qc.setQueryData(keys.story(path.workspaceId, s.id), s)
      void qc.invalidateQueries({ queryKey: keys.storyLists(path.workspaceId) })
    },
  }).mutate()
}

/**
 * Rejects a deliverable outside a component (one of a batch) and stores
 * the story it returns, through the mutation cache like approveDeliverable.
 */
export function rejectDeliverable(
  qc: QueryClient,
  path: { workspaceId: string; storyId: string; deliverableId: string }
): Promise<Story> {
  return new MutationObserver(qc, {
    mutationFn: () => call(postAction("reject", path)),
    onSuccess: (s: Story) => {
      qc.setQueryData(keys.story(path.workspaceId, s.id), s)
      void qc.invalidateQueries({ queryKey: keys.storyLists(path.workspaceId) })
    },
  }).mutate()
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

/**
 * Picks the thumbnail variant a long video goes out with; 0 goes back to
 * the recommended one.
 */
export function usePickThumbnail(workspaceId: string, storyId: string, deliverableId: string) {
  const store = useStoreStory(workspaceId)
  return useMutation({
    mutationFn: (variant: number) =>
      call(
        api.PUT("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/thumbnail", {
          params: { path: { workspaceId, storyId, deliverableId } },
          body: { variant },
        })
      ),
    onSuccess: store,
  })
}

/**
 * Changes one variant's words as a new revision whose thumbnails alone
 * are drawn again. 409 when `revision` is no longer the latest.
 */
export function useEditThumbnail(workspaceId: string, storyId: string, deliverableId: string) {
  const qc = useQueryClient()
  const store = useStoreStory(workspaceId)
  return useMutation({
    mutationFn: ({ n, revision, text }: { n: number; revision: number; text: string }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/thumbnails/{n}", {
          params: { path: { workspaceId, storyId, deliverableId, n } },
          body: { revision, text },
        })
      ),
    onSuccess: (story) => {
      store(story)
      void qc.invalidateQueries({ queryKey: keys.revisions(workspaceId, storyId, deliverableId) })
    },
    onError: (err) => {
      if (isApiError(err) && err.status === 409) {
        void qc.invalidateQueries({ queryKey: keys.story(workspaceId, storyId), exact: true })
      }
    },
  })
}

/**
 * Saves a person's note on one revision, replacing the one it had; blank
 * text removes it. The revisions on screen take the answer at once.
 */
export function useAnnotateRevision(workspaceId: string, storyId: string, deliverableId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ number, text }: { number: number; text: string }) =>
      call(
        api.PUT(
          "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/revisions/{number}/annotation",
          { params: { path: { workspaceId, storyId, deliverableId, number } }, body: { text } }
        )
      ),
    onSuccess: (rev: Revision) => {
      qc.setQueriesData<Revision[]>(
        { queryKey: keys.revisions(workspaceId, storyId, deliverableId) },
        (old) => old?.map((r) => (r.number === rev.number ? rev : r))
      )
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
      const keep = (s: Story) => s.id !== storyId
      qc.setQueriesData<StoryPages>({ queryKey: keys.storiesPaged(workspaceId) }, (old) =>
        old && { ...old, pages: old.pages.map((p) => ({ ...p, stories: p.stories.filter(keep) })) }
      )
      qc.setQueriesData<StoryWindow>({ queryKey: keys.storyWindows(workspaceId) }, (old) =>
        old && { ...old, stories: old.stories.filter(keep) }
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

/**
 * The moments of a long video that could be shorts, its narration's
 * sentences and the shorts limits. `blocked` says why none can be cut
 * (yet). Keyed by the shorts the story holds as well, so a short made
 * elsewhere (the pipeline, another reviewer) fetches them again.
 */
export const momentsQuery = (workspaceId: string, storyId: string, deliverableId: string, shorts: number) =>
  queryOptions({
    queryKey: [...keys.moments(workspaceId, storyId, deliverableId), { shorts }] as const,
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/moments", {
          params: { path: { workspaceId, storyId, deliverableId } },
          signal,
        })
      ),
  })

function useRefreshMoments(workspaceId: string, storyId: string, deliverableId: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: keys.moments(workspaceId, storyId, deliverableId) })
}

/** Queues the agent to suggest moments; resolves to the scan job to follow. */
export function useScanMoments(workspaceId: string, storyId: string, deliverableId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      call(
        api.POST("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/moments/scan", {
          params: { path: { workspaceId, storyId, deliverableId } },
        })
      ),
    onSuccess: () => void qc.invalidateQueries({ queryKey: keys.jobs(workspaceId) }),
  })
}

/** Adds a moment a person chose; the server snaps it to sentences. */
export function useAddMoment(workspaceId: string, storyId: string, deliverableId: string) {
  const refresh = useRefreshMoments(workspaceId, storyId, deliverableId)
  return useMutation({
    mutationFn: (body: MomentInput) =>
      call(
        api.POST("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/moments", {
          params: { path: { workspaceId, storyId, deliverableId } },
          body,
        })
      ),
    onSuccess: refresh,
  })
}

/** Moves or renames a suggested moment of the long video deliverableId. */
export function useAdjustMoment(workspaceId: string, storyId: string, deliverableId: string) {
  const refresh = useRefreshMoments(workspaceId, storyId, deliverableId)
  return useMutation({
    mutationFn: ({ momentId, body }: { momentId: string; body: MomentPatch }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/stories/{storyId}/moments/{momentId}", {
          params: { path: { workspaceId, storyId, momentId } },
          body,
        })
      ),
    onSuccess: refresh,
  })
}

/** Sets a suggested moment aside. */
export function useDiscardMoment(workspaceId: string, storyId: string, deliverableId: string) {
  const refresh = useRefreshMoments(workspaceId, storyId, deliverableId)
  return useMutation({
    mutationFn: async (momentId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/stories/{storyId}/moments/{momentId}", {
          params: { path: { workspaceId, storyId, momentId } },
        })
      )
    },
    onSuccess: refresh,
  })
}

/** Makes a short from each chosen moment and queues their drafts. */
export function useMakeShorts(workspaceId: string, storyId: string, deliverableId: string) {
  const store = useStoreStory(workspaceId)
  const refresh = useRefreshMoments(workspaceId, storyId, deliverableId)
  return useMutation({
    mutationFn: (momentIds: string[]) =>
      call(
        api.POST("/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/shorts", {
          params: { path: { workspaceId, storyId, deliverableId } },
          body: { momentIds },
        })
      ),
    onSuccess: (s) => {
      store(s)
      refresh()
    },
  })
}
