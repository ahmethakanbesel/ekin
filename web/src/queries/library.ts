import {
  infiniteQueryOptions,
  queryOptions,
  useMutation,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query"

import {
  API_BASE,
  api,
  call,
  type LibraryAsset,
  type LibraryAssetDetails,
  type LibraryAssetKind,
  type LibrarySettings,
} from "@/api/client"
import { uploadLibraryAsset, type UploadOptions } from "@/api/upload"
import { keys } from "./keys"
import {
  cursorPaging,
  keepWhileSearching,
  pageRows,
  type Cursor,
} from "./paging"

/** Uploads are processed in the background: poll while any is. */
const POLL_PROCESSING_MS = 2_000

/** Assets per page of the library. */
export const LIBRARY_PAGE_SIZE = 48

type AssetPage = { assets: LibraryAsset[]; nextCursor?: string }
type AssetPages = InfiniteData<AssetPage, Cursor>

const anyProcessing = (assets: LibraryAsset[]) =>
  assets.some((a) => a.status === "processing")

export type LibraryFilter = {
  kind: LibraryAssetKind | "all"
  q: string
  archived: boolean
}

/**
 * The library, newest first, a page at a time; `q` searches titles, tags
 * and descriptions on the server. The data is the loaded assets, flattened.
 */
export const libraryQuery = (
  workspaceId: string,
  { kind, q, archived }: LibraryFilter
) => {
  const queryKey = keys.libraryList(workspaceId, {
    kind,
    q,
    archived,
    limit: LIBRARY_PAGE_SIZE,
  })
  return infiniteQueryOptions({
    queryKey,
    queryFn: ({ pageParam, signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/library/assets", {
          params: {
            path: { workspaceId },
            query: {
              kind: kind === "all" ? undefined : [kind],
              q: q || undefined,
              archived: archived || undefined,
              limit: LIBRARY_PAGE_SIZE,
              cursor: pageParam,
            },
          },
          signal,
        })
      ),
    ...cursorPaging,
    select: (data: AssetPages) => pageRows(data.pages, (p) => p.assets),
    placeholderData: keepWhileSearching<AssetPages>(queryKey),
    refetchInterval: (query) =>
      query.state.data?.pages.some((p) => anyProcessing(p.assets))
        ? POLL_PROCESSING_MS
        : false,
  })
}

/** How many ready, unarchived assets of a kind the library holds, up to a hundred. */
const SIZE_LIMIT = 100

export const librarySizeQuery = (workspaceId: string, kind: LibraryAssetKind) =>
  queryOptions({
    queryKey: [...keys.libraryLists(workspaceId), "size", kind] as const,
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/library/assets", {
          params: {
            path: { workspaceId },
            query: { kind: [kind], status: ["ready"], limit: SIZE_LIMIT },
          },
          signal,
        })
      )
      return { count: res.assets.length, more: !!res.nextCursor }
    },
  })

export const libraryAssetQuery = (workspaceId: string, assetId: string) =>
  queryOptions({
    queryKey: keys.libraryAsset(workspaceId, assetId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/library/assets/{assetId}", {
          params: { path: { workspaceId, assetId } },
          signal,
        })
      ),
    refetchInterval: (q) =>
      q.state.data?.status === "processing" ? POLL_PROCESSING_MS : false,
  })

/** The revisions that used an asset, newest first. */
export const libraryUsagesQuery = (workspaceId: string, assetId: string) =>
  queryOptions({
    queryKey: [...keys.libraryAsset(workspaceId, assetId), "usages"] as const,
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/library/assets/{assetId}/usages", {
          params: { path: { workspaceId, assetId } },
          signal,
        })
      )
      return res.usages
    },
  })

/**
 * Where an asset's file is served (same origin, session cookie). The
 * asset's updatedAt busts the browser cache when it is processed again.
 */
export function libraryFileUrl(
  workspaceId: string,
  asset: Pick<LibraryAsset, "id" | "updatedAt">,
  file: "main" | "poster" | "preview"
): string {
  return `${API_BASE}/workspaces/${encodeURIComponent(workspaceId)}/library/assets/${encodeURIComponent(asset.id)}/files/${file}?v=${encodeURIComponent(asset.updatedAt)}`
}

/**
 * What to play for a music asset: a track itself, or the rendered start
 * of a composed score (whose main file is its code).
 */
export function listenUrl(
  workspaceId: string,
  asset: Pick<LibraryAsset, "id" | "updatedAt" | "origin">
): string {
  return libraryFileUrl(workspaceId, asset, asset.origin === "generated" ? "preview" : "main")
}

/** Which kinds the workspace saves to its library from its drafts. */
export const librarySettingsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: [...keys.library(workspaceId), "settings"] as const,
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/library/settings", {
          params: { path: { workspaceId } },
          signal,
        })
      ),
  })

export function useSaveLibrarySettings(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: LibrarySettings) =>
      call(
        api.PUT("/workspaces/{workspaceId}/library/settings", {
          params: { path: { workspaceId } },
          body,
        })
      ),
    onSuccess: (saved) => qc.setQueryData([...keys.library(workspaceId), "settings"], saved),
  })
}

type RevisionRef = { storyId: string; deliverableId: string; number: number }

const revisionMediaKey = (workspaceId: string, r: RevisionRef) =>
  [...keys.revisions(workspaceId, r.storyId, r.deliverableId), r.number, "library"] as const

/** The stock and music a video revision uses, and what of it the library holds. */
export const revisionMediaQuery = (workspaceId: string, r: RevisionRef) =>
  queryOptions({
    queryKey: revisionMediaKey(workspaceId, r),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET(
          "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/revisions/{number}/library",
          { params: { path: { workspaceId, ...r } }, signal }
        )
      )
      return res.media
    },
  })

/** Saves one of a revision's stock items or its score to the library. */
export function useSaveRevisionMedia(workspaceId: string, r: RevisionRef) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (key: string) =>
      call(
        api.POST(
          "/workspaces/{workspaceId}/stories/{storyId}/deliverables/{deliverableId}/revisions/{number}/library",
          { params: { path: { workspaceId, ...r } }, body: { key } }
        )
      ),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: revisionMediaKey(workspaceId, r) })
      void qc.invalidateQueries({ queryKey: keys.libraryLists(workspaceId) })
    },
  })
}

/**
 * Writes a changed asset into its detail cache and the lists, then
 * refreshes the lists (a new or archived asset may join or leave them).
 */
function useStoreAsset(workspaceId: string) {
  const qc = useQueryClient()
  return (a: LibraryAsset) => {
    qc.setQueryData(keys.libraryAsset(workspaceId, a.id), a)
    qc.setQueriesData<AssetPages>(
      { queryKey: keys.libraryLists(workspaceId) },
      (old) =>
        old && {
          ...old,
          pages: old.pages.map((p) => ({
            ...p,
            assets: p.assets.map((x) => (x.id === a.id ? a : x)),
          })),
        }
    )
    void qc.invalidateQueries({ queryKey: keys.libraryLists(workspaceId) })
  }
}

export function useUploadLibraryAsset(workspaceId: string) {
  const store = useStoreAsset(workspaceId)
  return useMutation({
    mutationFn: ({ file, ...opts }: { file: File } & UploadOptions) =>
      uploadLibraryAsset(workspaceId, file, opts),
    onSuccess: store,
  })
}

export function useUpdateLibraryAsset(workspaceId: string) {
  const store = useStoreAsset(workspaceId)
  return useMutation({
    mutationFn: ({
      assetId,
      details,
      archived,
    }: {
      assetId: string
      details?: LibraryAssetDetails
      archived?: boolean
    }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/library/assets/{assetId}", {
          params: { path: { workspaceId, assetId } },
          body: { details, archived },
        })
      ),
    onSuccess: store,
  })
}

export function useRetryLibraryAsset(workspaceId: string) {
  const store = useStoreAsset(workspaceId)
  return useMutation({
    mutationFn: (assetId: string) =>
      call(
        api.POST("/workspaces/{workspaceId}/library/assets/{assetId}/retry", {
          params: { path: { workspaceId, assetId } },
        })
      ),
    onSuccess: store,
  })
}

export function useDeleteLibraryAsset(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (assetId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/library/assets/{assetId}", {
          params: { path: { workspaceId, assetId } },
        })
      )
      return assetId
    },
    onSuccess: (assetId) => {
      qc.setQueriesData<AssetPages>(
        { queryKey: keys.libraryLists(workspaceId) },
        (old) =>
          old && {
            ...old,
            pages: old.pages.map((p) => ({
              ...p,
              assets: p.assets.filter((a) => a.id !== assetId),
            })),
          }
      )
      qc.removeQueries({ queryKey: keys.libraryAsset(workspaceId, assetId) })
      void qc.invalidateQueries({ queryKey: keys.libraryLists(workspaceId) })
    },
  })
}
