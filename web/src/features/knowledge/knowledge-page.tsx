import { useRef, useState, type DragEvent } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { AlertCircleIcon, LibraryIcon, UploadIcon, XIcon } from "lucide-react"

import type { Document } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  PageHeader,
  ReadOnlyNote,
  TableSkeleton,
} from "@/components/page"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatBytes, formatInteger } from "@/lib/format"
import { formatDateTime } from "@/lib/time"
import { cn } from "@/lib/utils"
import { documentsQuery } from "@/queries/documents"
import { FormatBadge, StatusBadge } from "./badges"
import { DocumentMenu } from "./document-actions"
import { tokensLabel } from "./tokens"
import { ACCEPTED_EXTENSIONS, useUploads, type UploadItem } from "./use-uploads"

export function KnowledgePage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const list = useQuery(documentsQuery(workspace.id))
  const uploads = useUploads(workspace.id)
  const input = useRef<HTMLInputElement>(null)
  const drop = useFileDrop(canEdit, uploads.add)

  const uploadButton = canEdit ? (
    <Button onClick={() => input.current?.click()}>
      <UploadIcon data-icon="inline-start" />
      {m.knowledge.upload}
    </Button>
  ) : null

  let body
  if (list.isPending) body = <TableSkeleton />
  else if (list.isError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0)
    body = (
      <EmptyState
        icon={<LibraryIcon />}
        title={m.knowledge.emptyTitle}
        body={canEdit ? m.knowledge.emptyBody : m.knowledge.emptyBodyReadOnly}
        action={
          canEdit ? (
            <div className="flex flex-col items-center gap-2">
              {uploadButton}
              <p className="text-xs text-muted-foreground">
                {m.knowledge.dropHint}
              </p>
            </div>
          ) : null
        }
      />
    )
  else
    body = (
      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">
                {m.knowledge.columns.title}
              </TableHead>
              <TableHead>{m.knowledge.columns.format}</TableHead>
              <TableHead className="text-right">
                {m.knowledge.columns.pages}
              </TableHead>
              <TableHead className="text-right">
                {m.knowledge.columns.tokens}
              </TableHead>
              <TableHead className="text-right">
                {m.knowledge.columns.size}
              </TableHead>
              <TableHead>{m.knowledge.columns.status}</TableHead>
              <TableHead>{m.knowledge.columns.uploaded}</TableHead>
              <TableHead className="w-12 pr-4">
                <span className="sr-only">{m.common.actions}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data.map((d) => (
              <DocumentRow
                key={d.id}
                doc={d}
                workspaceId={workspace.id}
                timezone={workspace.timezone}
                canEdit={canEdit}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    )

  return (
    <div className="relative" {...drop.handlers}>
      <PageHeader
        title={m.knowledge.title}
        description={m.knowledge.description}
        actions={list.data?.length ? uploadButton : null}
      />
      {canEdit ? (
        <input
          ref={input}
          type="file"
          multiple
          hidden
          accept={ACCEPTED_EXTENSIONS.join(",")}
          onChange={(e) => {
            if (e.target.files) uploads.add(e.target.files)
            e.target.value = ""
          }}
        />
      ) : (
        <ReadOnlyNote>{m.knowledge.readOnlyHint}</ReadOnlyNote>
      )}
      <UploadQueue
        items={uploads.items}
        onCancel={uploads.cancel}
        onDismiss={uploads.dismiss}
      />
      {body}
      {drop.over ? (
        <div className="pointer-events-none absolute -inset-2 z-10 flex items-center justify-center rounded-xl border-2 border-dashed border-primary-ink bg-background/85 backdrop-blur-sm">
          <div className="flex flex-col items-center gap-2 text-primary-ink">
            <UploadIcon className="size-8" />
            <p className="font-heading text-base font-medium">
              {m.knowledge.dropTitle}
            </p>
          </div>
        </div>
      ) : null}
    </div>
  )
}

const hasFiles = (e: DragEvent) => e.dataTransfer.types.includes("Files")

/** Drag-and-drop of files onto the page, for editors. */
function useFileDrop(enabled: boolean, onFiles: (files: FileList) => void) {
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

function UploadQueue({
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
    <section aria-label={m.knowledge.uploadsTitle} className="mb-4 grid gap-2">
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
                {m.knowledge.waiting}
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
                    ? m.knowledge.processing
                    : m.knowledge.uploading(Math.round(it.progress * 100))}
                </span>
              </div>
            )}
          </div>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={
              it.state === "failed"
                ? m.knowledge.dismiss
                : m.knowledge.cancelUpload
            }
            title={
              it.state === "failed"
                ? m.knowledge.dismiss
                : m.knowledge.cancelUpload
            }
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

function DocumentRow({
  doc: d,
  workspaceId,
  timezone,
  canEdit,
}: {
  doc: Document
  workspaceId: string
  timezone: string
  canEdit: boolean
}) {
  const ready = d.status === "ready"
  return (
    <TableRow>
      <TableCell className="max-w-md pl-4 align-top">
        <div className="flex min-w-0 flex-col gap-0.5">
          <Link
            to="/w/$workspaceId/knowledge/$documentId"
            params={{ workspaceId, documentId: d.id }}
            className="truncate font-medium hover:underline"
          >
            {d.title}
          </Link>
          <span className="truncate text-xs text-muted-foreground">
            {d.filename}
          </span>
          {d.status === "failed" && d.failure ? (
            <span
              className="line-clamp-2 text-xs whitespace-normal text-destructive"
              title={d.failure}
            >
              {d.failure}
            </span>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="align-top">
        <FormatBadge format={d.format} />
      </TableCell>
      <TableCell className="tabular text-right align-top">
        {ready ? formatInteger(d.pageCount) : m.common.none}
      </TableCell>
      <TableCell className="tabular text-right align-top whitespace-nowrap">
        {ready ? tokensLabel(d.tokens) : m.common.none}
      </TableCell>
      <TableCell className="tabular text-right align-top whitespace-nowrap">
        {formatBytes(d.size)}
      </TableCell>
      <TableCell className="align-top">
        <StatusBadge status={d.status} />
      </TableCell>
      <TableCell className="tabular align-top text-sm whitespace-nowrap">
        {formatDateTime(d.createdAt, timezone)}
      </TableCell>
      <TableCell className="pr-4 align-top">
        {canEdit ? (
          <div className="flex justify-end">
            <DocumentMenu workspaceId={workspaceId} doc={d} showOpen />
          </div>
        ) : null}
      </TableCell>
    </TableRow>
  )
}
