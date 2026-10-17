import { m } from "@/i18n"
import { extensionOf, useUploadQueue } from "@/lib/use-upload-queue"
import { useUploadDocument } from "@/queries/documents"

/** The server's limit (domain MaxFileBytes). */
export const MAX_UPLOAD_BYTES = 100 * 1024 * 1024

/** Extensions the server accepts (domain FormatFromFilename). */
export const ACCEPTED_EXTENSIONS = [
  ".pdf",
  ".txt",
  ".text",
  ".md",
  ".markdown",
  ".epub",
]

function precheck(file: File): string | undefined {
  if (!ACCEPTED_EXTENSIONS.includes(extensionOf(file.name)))
    return m.knowledge.unsupported
  if (file.size > MAX_UPLOAD_BYTES) return m.knowledge.tooLarge
  return undefined
}

/** The knowledge base's upload queue (the document shows up as "Extracting"). */
export function useUploads(workspaceId: string) {
  const upload = useUploadDocument(workspaceId)
  return useUploadQueue({
    upload: (file, opts) => upload.mutateAsync({ file, ...opts }),
    precheck,
  })
}
