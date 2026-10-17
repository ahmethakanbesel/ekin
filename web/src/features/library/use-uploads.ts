import type { LibraryAssetKind } from "@/api/client"
import { m } from "@/i18n"
import { extensionOf, useUploadQueue } from "@/lib/use-upload-queue"
import { useUploadLibraryAsset } from "@/queries/library"

const MB = 1024 * 1024

/** Extensions and size limits per kind (domain KindFromFilename, Kind.MaxBytes). */
const KINDS: Record<
  LibraryAssetKind,
  { extensions: string[]; maxBytes: number }
> = {
  video: { extensions: [".mp4", ".mov", ".m4v", ".webm"], maxBytes: 300 * MB },
  photo: { extensions: [".jpg", ".jpeg", ".png", ".webp"], maxBytes: 25 * MB },
  music: {
    extensions: [".mp3", ".wav", ".flac", ".m4a", ".aac", ".ogg", ".opus"],
    maxBytes: 50 * MB,
  },
}

/** Every extension the library accepts, for the file picker. */
export const ACCEPTED_EXTENSIONS = Object.values(KINDS).flatMap(
  (k) => k.extensions
)

/** The kind a file name stands for, as the server will decide it. */
export function kindOf(name: string): LibraryAssetKind | undefined {
  const ext = extensionOf(name)
  return (Object.keys(KINDS) as LibraryAssetKind[]).find((k) =>
    KINDS[k].extensions.includes(ext)
  )
}

function precheck(file: File): string | undefined {
  const kind = kindOf(file.name)
  if (!kind) return m.library.unsupported
  if (file.size > KINDS[kind].maxBytes)
    return m.library.tooLarge(KINDS[kind].maxBytes / MB)
  return undefined
}

/** The library's upload queue; each file's kind comes from its name. */
export function useUploads(workspaceId: string) {
  const upload = useUploadLibraryAsset(workspaceId)
  return useUploadQueue({
    upload: (file, opts) => upload.mutateAsync({ file, ...opts }),
    precheck,
  })
}
