import { useRef, useState, type DragEvent, type ReactNode } from "react"
import { AlertCircleIcon, UploadIcon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { m } from "@/i18n"
import { formatBytes } from "@/lib/format"
import type { UploadItem } from "@/lib/use-upload-queue"
import { cn } from "@/lib/utils"

const hasFiles = (e: DragEvent) => e.dataTransfer.types.includes("Files")

/** Drag-and-drop of files onto a page, for editors. */
export function useFileDrop(
  enabled: boolean,
  onFiles: (files: FileList) => void
) {
  const [over, setOver] = useState(false)
  // dragenter/dragleave fire for every child crossed; count to know when
  // the pointer really left.
  const depth = useRef(0)

  if (!enabled) return { over: false, handlers: {} }
  return {
    over,
    handlers: {
      onDragEnter: (e: DragEvent) => {
        if (!hasFiles(e)) return
        e.preventDefault()
        depth.current++
        setOver(true)
      },
      onDragOver: (e: DragEvent) => {
        if (!hasFiles(e)) return
        e.preventDefault()
        e.dataTransfer.dropEffect = "copy"
      },
      onDragLeave: (e: DragEvent) => {
        if (!hasFiles(e)) return
        depth.current = Math.max(0, depth.current - 1)
        if (depth.current === 0) setOver(false)
      },
      onDrop: (e: DragEvent) => {
        if (!hasFiles(e)) return
        e.preventDefault()
        depth.current = 0
        setOver(false)
        if (e.dataTransfer.files.length) onFiles(e.dataTransfer.files)
      },
    },
  }
}

/** The veil over a page while files are dragged onto it. */
export function DropOverlay({ children }: { children: ReactNode }) {
  return (
    <div className="pointer-events-none absolute -inset-2 z-10 flex items-center justify-center rounded-xl border-2 border-dashed border-foreground/50 bg-background/85 backdrop-blur-sm">
      <div className="flex flex-col items-center gap-2 text-foreground">
        <UploadIcon className="size-8" />
        <p className="font-heading text-base font-medium">{children}</p>
      </div>
    </div>
  )
}

/** Uploads in flight, waiting or failed, each with its cancel or dismiss. */
export function UploadQueue({
  items,
  onCancel,
  onDismiss,
}: {
  items: UploadItem[]
  onCancel: (key: string) => void
  onDismiss: (key: string) => void
}) {
  if (items.length === 0) return null
  return (
    <section aria-label={m.uploads.title} className="mb-4 grid gap-2">
      {items.map((it) => (
        <div
          key={it.key}
          className={cn(
            "flex items-center gap-3 rounded-xl border px-4 py-2.5 text-sm",
            it.state === "failed" && "border-destructive/30 bg-destructive/5"
          )}
        >
          {it.state === "failed" ? (
            <AlertCircleIcon className="size-4 shrink-0 text-destructive" />
          ) : (
            <UploadIcon className="size-4 shrink-0 text-muted-foreground" />
          )}
          <div className="grid min-w-0 flex-1 gap-1.5">
            <div className="flex min-w-0 items-baseline gap-2">
              <span className="truncate font-medium">{it.name}</span>
              <span className="tabular shrink-0 text-xs text-muted-foreground">
                {formatBytes(it.size)}
              </span>
            </div>
            {it.state === "failed" ? (
              <p className="text-xs text-destructive">{it.error}</p>
            ) : it.state === "waiting" ? (
              <p className="text-xs text-muted-foreground">
                {m.uploads.waiting}
              </p>
            ) : (
              <div className="flex items-center gap-3">
                <Progress
                  value={Math.round(it.progress * 100)}
                  aria-label={it.name}
                  className="flex-1 [&_[data-slot=progress-track]]:h-1.5"
                />
                <span className="tabular w-32 shrink-0 text-right text-xs text-muted-foreground">
                  {it.progress >= 1
                    ? m.uploads.processing
                    : m.uploads.uploading(Math.round(it.progress * 100))}
                </span>
              </div>
            )}
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={
              it.state === "failed" ? m.uploads.dismiss : m.uploads.cancel
            }
            title={it.state === "failed" ? m.uploads.dismiss : m.uploads.cancel}
            onClick={() =>
              it.state === "failed" ? onDismiss(it.key) : onCancel(it.key)
            }
          >
            <XIcon />
          </Button>
        </div>
      ))}
    </section>
  )
}
