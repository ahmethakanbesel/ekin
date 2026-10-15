import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query"

import { API_BASE, api, call, type Document } from "@/api/client"
import { uploadDocument, type UploadOptions } from "@/api/upload"
import { keys } from "./keys"

/** Pages come from the server in chunks of this many (its per-call limit). */
export const PAGE_CHUNK = 200

/** The chunk holding a 1-based page number. */
export const chunkOf = (page: number) => Math.floor((page - 1) / PAGE_CHUNK)

/** Extraction runs in the background: poll while any document waits for it. */
const POLL_PENDING_MS = 2_000

export const documentsQuery = (workspaceId: string) =>
  queryOptions({
    queryKey: keys.documents(workspaceId),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/documents", {
          params: { path: { workspaceId } },
          signal,
        }),
      )
      return res.documents
    },
    refetchInterval: (q) =>
      q.state.data?.some((d) => d.status === "pending") ? POLL_PENDING_MS : false,
  })

export const documentQuery = (workspaceId: string, documentId: string) =>
  queryOptions({
    queryKey: keys.document(workspaceId, documentId),
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/documents/{documentId}", {
          params: { path: { workspaceId, documentId } },
          signal,
        }),
      ),
    refetchInterval: (q) => (q.state.data?.status === "pending" ? POLL_PENDING_MS : false),
  })

/**
 * One chunk of a ready document's pages (chunk 0 = pages 1–200). The pages
 * of a ready document never change, so a fetched chunk stays fresh.
 */
export const documentPagesQuery = (workspaceId: string, documentId: string, chunk: number) =>
  queryOptions({
    queryKey: keys.documentPages(workspaceId, documentId, chunk),
    queryFn: async ({ signal }) => {
      const from = chunk * PAGE_CHUNK + 1
      const res = await call(
        api.GET("/workspaces/{workspaceId}/documents/{documentId}/pages", {
          params: {
            path: { workspaceId, documentId },
            query: { from, to: from + PAGE_CHUNK - 1 },
          },
          signal,
        }),
      )
      return res.pages
    },
    staleTime: Infinity,
  })

/**
 * A ready document's table of contents (empty when the file declares
 * none). Keyed by the document's updatedAt, so extracting it again
 * fetches the new one.
 */
export const documentContentsQuery = (workspaceId: string, doc: Pick<Document, "id" | "updatedAt">) =>
  queryOptions({
    queryKey: keys.documentContents(workspaceId, doc.id, doc.updatedAt),
    queryFn: async ({ signal }) => {
      const res = await call(
        api.GET("/workspaces/{workspaceId}/documents/{documentId}/contents", {
          params: { path: { workspaceId, documentId: doc.id } },
          signal,
        }),
      )
      return res.sections
    },
    staleTime: Infinity,
  })

/** Where the original file is served (same origin, session cookie). */
export function documentFileUrl(workspaceId: string, documentId: string): string {
  return `${API_BASE}/workspaces/${encodeURIComponent(workspaceId)}/documents/${encodeURIComponent(documentId)}/file`
}

/**
 * The original file as a Blob. The API answers with X-Frame-Options: DENY,
 * so the PDF tab frames an object URL of this rather than the API URL.
 */
export const documentFileQuery = (workspaceId: string, documentId: string) =>
  queryOptions({
    queryKey: [...keys.document(workspaceId, documentId), "file"] as const,
    queryFn: ({ signal }) =>
      call(
        api.GET("/workspaces/{workspaceId}/documents/{documentId}/file", {
          params: { path: { workspaceId, documentId } },
          parseAs: "blob",
          signal,
        }),
      ),
    staleTime: Infinity,
    // Originals can be large; do not keep one around after the tab closes.
    gcTime: 30_000,
  })

/** Writes a changed document into the detail cache and refreshes the list. */
function useStoreDocument(workspaceId: string) {
  const qc = useQueryClient()
  return (d: Document) => {
    qc.setQueryData(keys.document(workspaceId, d.id), d)
    qc.setQueryData<Document[]>(keys.documents(workspaceId), (old) =>
      old?.some((x) => x.id === d.id) ? old.map((x) => (x.id === d.id ? d : x)) : old && [d, ...old],
    )
    void qc.invalidateQueries({ queryKey: keys.documents(workspaceId), exact: true })
  }
}

export function useUploadDocument(workspaceId: string) {
  const store = useStoreDocument(workspaceId)
  return useMutation({
    mutationFn: ({ file, ...opts }: { file: File } & UploadOptions) =>
      uploadDocument(workspaceId, file, opts),
    onSuccess: store,
  })
}

export function useRenameDocument(workspaceId: string) {
  const store = useStoreDocument(workspaceId)
  return useMutation({
    mutationFn: ({ documentId, title }: { documentId: string; title: string }) =>
      call(
        api.PATCH("/workspaces/{workspaceId}/documents/{documentId}", {
          params: { path: { workspaceId, documentId } },
          body: { title },
        }),
      ),
    onSuccess: store,
  })
}

export function useRetryDocument(workspaceId: string) {
  const store = useStoreDocument(workspaceId)
  return useMutation({
    mutationFn: (documentId: string) =>
      call(
        api.POST("/workspaces/{workspaceId}/documents/{documentId}/retry", {
          params: { path: { workspaceId, documentId } },
        }),
      ),
    onSuccess: store,
  })
}

/** Deletes a document. A document a pipeline references answers 409 Conflict. */
export function useDeleteDocument(workspaceId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (documentId: string) => {
      await call(
        api.DELETE("/workspaces/{workspaceId}/documents/{documentId}", {
          params: { path: { workspaceId, documentId } },
        }),
      )
      return documentId
    },
    onSuccess: (documentId) => {
      qc.setQueryData<Document[]>(keys.documents(workspaceId), (old) =>
        old?.filter((d) => d.id !== documentId),
      )
      // Also drops its pages and file, which sit under the same key. An open
      // viewer keeps its (active) queries until it navigates away, rather
      // than refetching them into 404s.
      qc.removeQueries({ queryKey: keys.document(workspaceId, documentId), type: "inactive" })
      void qc.invalidateQueries({ queryKey: keys.documents(workspaceId), exact: true })
    },
  })
}
