import { API_BASE, ApiError, toApiError, type Document } from "./client"

export type UploadOptions = {
  title?: string
  /** Called with the fraction of the request body sent so far (0–1). */
  onProgress?: (fraction: number) => void
  signal?: AbortSignal
}

/**
 * Uploads one file to the knowledge base. XMLHttpRequest rather than
 * fetch, because only XHR reports upload progress. The browser writes the
 * multipart Content-Type (with its boundary) itself.
 */
export function uploadDocument(
  workspaceId: string,
  file: File,
  { title, onProgress, signal }: UploadOptions = {},
): Promise<Document> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Upload cancelled", "AbortError"))
      return
    }
    const xhr = new XMLHttpRequest()
    xhr.open("POST", `${API_BASE}/workspaces/${encodeURIComponent(workspaceId)}/documents`)
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
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as Document)
      else reject(toApiError(xhr.status, body, xhr.statusText))
    })
    xhr.addEventListener("error", () => reject(new ApiError(0, "NetworkError", "upload failed")))
    xhr.addEventListener("abort", () => reject(new DOMException("Upload cancelled", "AbortError")))
    signal?.addEventListener("abort", () => xhr.abort(), { once: true })

    const form = new FormData()
    // The server reads the parts in order: the title must precede the file.
    if (title?.trim()) form.append("title", title.trim())
    form.append("file", file, file.name)
    xhr.send(form)
  })
}
