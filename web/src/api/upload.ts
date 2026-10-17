import {
  API_BASE,
  ApiError,
  toApiError,
  type Document,
  type LibraryAsset,
  type LibraryAssetDetails,
  type LibraryAssetKind,
} from "./client"

export type UploadOptions = {
  /** Called with the fraction of the request body sent so far (0–1). */
  onProgress?: (fraction: number) => void
  signal?: AbortSignal
}

/**
 * Posts a multipart form. XMLHttpRequest rather than fetch, because only
 * XHR reports upload progress. The browser writes the multipart
 * Content-Type (with its boundary) itself.
 */
function sendForm<T>(
  path: string,
  form: FormData,
  { onProgress, signal }: UploadOptions
): Promise<T> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Upload cancelled", "AbortError"))
      return
    }
    const xhr = new XMLHttpRequest()
    xhr.open("POST", `${API_BASE}${path}`)
    xhr.upload.addEventListener("progress", (e) => {
      if (e.lengthComputable && e.total > 0) onProgress?.(e.loaded / e.total)
    })
    xhr.addEventListener("load", () => {
      let body: unknown = xhr.responseText
      try {
        body = JSON.parse(xhr.responseText)
      } catch {
        // not JSON: keep the text for the error message
      }
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as T)
      else reject(toApiError(xhr.status, body, xhr.statusText))
    })
    xhr.addEventListener("error", () =>
      reject(new ApiError(0, "NetworkError", "upload failed"))
    )
    xhr.addEventListener("abort", () =>
      reject(new DOMException("Upload cancelled", "AbortError"))
    )
    signal?.addEventListener("abort", () => xhr.abort(), { once: true })
    xhr.send(form)
  })
}

/** Uploads one file to the knowledge base. */
export function uploadDocument(
  workspaceId: string,
  file: File,
  { title, ...opts }: UploadOptions & { title?: string } = {}
): Promise<Document> {
  const form = new FormData()
  // The server reads the parts in order: the title must precede the file.
  if (title?.trim()) form.append("title", title.trim())
  form.append("file", file, file.name)
  return sendForm(
    `/workspaces/${encodeURIComponent(workspaceId)}/documents`,
    form,
    opts
  )
}

/**
 * Uploads one file to the media library. Without a kind the server takes
 * it from the file name; without details, the title too.
 */
export function uploadLibraryAsset(
  workspaceId: string,
  file: File,
  {
    kind,
    details,
    ...opts
  }: UploadOptions & {
    kind?: LibraryAssetKind
    details?: LibraryAssetDetails
  } = {}
): Promise<LibraryAsset> {
  const form = new FormData()
  // The server reads the parts in order: kind and details precede the file.
  if (kind) form.append("kind", kind)
  if (details) form.append("details", JSON.stringify(details))
  form.append("file", file, file.name)
  return sendForm(
    `/workspaces/${encodeURIComponent(workspaceId)}/library/assets`,
    form,
    opts
  )
}
