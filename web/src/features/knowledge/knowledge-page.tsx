import { useRef } from "react"
import { useInfiniteQuery } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { LibraryIcon, SearchIcon, UploadIcon } from "lucide-react"

import type { Document } from "@/api/client"
import {
  EmptyState,
  ErrorState,
  LoadMore,
  PageHeader,
  ReadOnlyNote,
  TableSkeleton,
} from "@/components/page"
import { SearchField } from "@/components/search-field"
import { DropOverlay, UploadQueue, useFileDrop } from "@/components/uploads"
import { Button } from "@/components/ui/button"
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
import { ACCEPTED_EXTENSIONS, useUploads } from "./use-uploads"

const ROUTE = "/w/$workspaceId/knowledge"

export function KnowledgePage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const q = search.q ?? ""
  const list = useInfiniteQuery(documentsQuery(workspace.id, { q }))
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
  else if (list.isLoadingError)
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0 && q)
    body = (
      <EmptyState
        icon={<SearchIcon />}
        title={m.common.noMatch(q)}
        body={m.common.noMatchBody}
      />
    )
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
      <>
        <div
          aria-busy={list.isPlaceholderData || undefined}
          className={cn(
            "rounded-xl border transition-opacity",
            list.isPlaceholderData && "opacity-60"
          )}
        >
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
        <LoadMore query={list} />
      </>
    )

  // The header's upload and the search box come with a list; a search
  // that matches nothing keeps them.
  const hasList = q !== "" || !!list.data?.length

  return (
    <div className="relative" {...drop.handlers}>
      <PageHeader
        title={m.knowledge.title}
        description={m.knowledge.description}
        actions={hasList ? uploadButton : null}
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
      {hasList ? (
        <SearchField
          className="mb-4"
          value={q}
          onChange={(next) =>
            void navigate({
              search: { q: next || undefined },
              replace: true,
            })
          }
          label={m.knowledge.search}
          placeholder={m.knowledge.searchPlaceholder}
        />
      ) : null}
      {body}
      {drop.over ? <DropOverlay>{m.knowledge.dropTitle}</DropOverlay> : null}
    </div>
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
