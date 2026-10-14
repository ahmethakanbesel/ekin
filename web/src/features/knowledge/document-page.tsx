import { useEffect, useRef, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router"
import {
  ArrowLeftIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  ExternalLinkIcon,
  FileTextIcon,
  Loader2Icon,
  RotateCwIcon,
} from "lucide-react"
import { toast } from "sonner"

import { isApiError, type Document } from "@/api/client"
import { Banner, EmptyState, ErrorState, PageHeader } from "@/components/page"
import { Button, buttonVariants } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { toInt } from "@/features/pipelines/model"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatBytes, formatCompact } from "@/lib/format"
import { cn } from "@/lib/utils"
import {
  PAGE_CHUNK,
  chunkOf,
  documentFileQuery,
  documentFileUrl,
  documentPagesQuery,
  documentQuery,
  useRetryDocument,
} from "@/queries/documents"
import { FormatBadge, StatusBadge } from "./badges"
import { DocumentMenu } from "./document-actions"
import type { DocumentView as View } from "./search"
import { tokensLabel, useTokenEstimates } from "./tokens"

const ROUTE = "/w/$workspaceId/knowledge/$documentId"

function BackLink({ workspaceId }: { workspaceId: string }) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className="mb-2 -ml-2"
      render={<Link to="/w/$workspaceId/knowledge" params={{ workspaceId }} />}
      nativeButton={false}
    >
      <ArrowLeftIcon data-icon="inline-start" />
      {m.nav.knowledge}
    </Button>
  )
}

export function DocumentPage() {
  const workspace = useWorkspace()
  const { documentId } = useParams({ from: ROUTE })
  const query = useQuery(documentQuery(workspace.id, documentId))

  if (query.isPending) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-96 w-full" />
      </div>
    )
  }
  if (query.isError) {
    if (isApiError(query.error) && (query.error.status === 404 || query.error.status === 400)) {
      return (
        <div className="max-w-5xl">
          <BackLink workspaceId={workspace.id} />
          <EmptyState title={m.knowledge.notFound} />
        </div>
      )
    }
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  }
  return <DocumentView doc={query.data} />
}

function DocumentView({ doc }: { doc: Document }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const navigate = useNavigate()
  const ready = doc.status === "ready"

  const meta = (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
      <FormatBadge format={doc.format} />
      {ready ? <span className="tabular">{m.knowledge.pages(doc.pageCount)}</span> : null}
      {ready ? <span className="tabular">{tokensLabel(doc.tokens)}</span> : null}
      <span className="tabular">{formatBytes(doc.size)}</span>
      <span className="truncate">{doc.filename}</span>
      {ready ? null : <StatusBadge status={doc.status} />}
    </span>
  )

  return (
    <div>
      <BackLink workspaceId={workspace.id} />
      <PageHeader
        title={doc.title}
        description={meta}
        actions={
          <>
            <a
              href={documentFileUrl(workspace.id, doc.id)}
              target="_blank"
              rel="noreferrer"
              className={buttonVariants({ variant: "outline" })}
            >
              <ExternalLinkIcon data-icon="inline-start" />
              {m.knowledge.openOriginal}
            </a>
            {canEdit ? (
              <DocumentMenu
                workspaceId={workspace.id}
                doc={doc}
                onDeleted={() =>
                  void navigate({
                    to: "/w/$workspaceId/knowledge",
                    params: { workspaceId: workspace.id },
                  })
                }
              />
            ) : null}
          </>
        }
      />
      {doc.status === "pending" ? (
        <div className="flex flex-col items-center gap-3 rounded-4xl border border-dashed px-6 py-14 text-center">
          <Loader2Icon className="size-6 animate-spin text-muted-foreground" />
          <div className="max-w-md space-y-1">
            <h2 className="font-heading text-base font-medium">{m.knowledge.pendingTitle}</h2>
            <p className="text-sm text-muted-foreground">{m.knowledge.pendingBody}</p>
          </div>
        </div>
      ) : doc.status === "failed" ? (
        <FailedBanner doc={doc} canEdit={canEdit} />
      ) : (
        // A new document id starts with a fresh range.
        <Reader key={doc.id} doc={doc} />
      )}
    </div>
  )
}

function FailedBanner({ doc, canEdit }: { doc: Document; canEdit: boolean }) {
  const workspace = useWorkspace()
  const retry = useRetryDocument(workspace.id)
  const doRetry = async () => {
    try {
      await retry.mutateAsync(doc.id)
      toast.success(m.knowledge.retried(doc.title))
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }
  return (
    <Banner
      title={m.knowledge.failedTitle}
      action={
        canEdit ? (
          <Button
            size="sm"
            variant="outline"
            disabled={retry.isPending}
            onClick={() => void doRetry()}
          >
            <RotateCwIcon data-icon="inline-start" />
            {m.knowledge.retry}
          </Button>
        ) : null
      }
    >
      <p className="whitespace-pre-wrap">{doc.failure || m.knowledge.noFailureReason}</p>
    </Banner>
  )
}

type Range = { from: string; to: string }

/** The pages of a ready document: range picker, page list, page text and PDF. */
function Reader({ doc }: { doc: Document }) {
  const workspace = useWorkspace()
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const total = doc.pageCount
  const page = Math.min(Math.max(search.page ?? 1, 1), Math.max(total, 1))
  const view: View = doc.format === "pdf" && search.view === "pdf" ? "pdf" : "text"
  const [range, setRange] = useState<Range>({ from: "1", to: String(total) })

  const chunk = chunkOf(page)
  const pages = useQuery(documentPagesQuery(workspace.id, doc.id, chunk))
  const current = pages.data?.find((p) => p.number === page)

  const from = toInt(range.from)
  const to = toInt(range.to)
  const rangeValid = from !== undefined && to !== undefined && from >= 1 && from <= to && to <= total

  const setPage = (n: number) =>
    void navigate({
      search: (s) => ({ ...s, page: n === 1 ? undefined : n }),
      replace: true,
    })
  const setView = (v: View) =>
    void navigate({
      search: (s) => ({ ...s, view: v === "pdf" ? "pdf" : undefined }),
      replace: true,
    })

  if (total === 0) {
    return <EmptyState icon={<FileTextIcon />} title={m.knowledge.viewer.emptyPage} />
  }

  const t = m.knowledge.viewer
  return (
    <div className="grid gap-4">
      <RangeBar
        doc={doc}
        page={page}
        range={range}
        valid={rangeValid}
        from={from}
        to={to}
        onChange={setRange}
      />
      <div className="grid items-start gap-4 lg:grid-cols-[15rem_minmax(0,1fr)]">
        <PageNavigator
          total={total}
          page={page}
          chunk={chunk}
          pages={pages.data}
          loading={pages.isPending}
          error={pages.isError ? pages.error : null}
          onRetry={() => void pages.refetch()}
          inRange={(n) => rangeValid && n >= from && n <= to}
          onSelect={setPage}
        />
        <Tabs
          value={view}
          onValueChange={(v) => setView(v === "pdf" ? "pdf" : "text")}
          className="min-w-0 gap-0 rounded-3xl border"
        >
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-4 py-2.5">
            {doc.format === "pdf" ? (
              <TabsList>
                <TabsTrigger value="text">{t.text}</TabsTrigger>
                <TabsTrigger value="pdf">{t.original}</TabsTrigger>
              </TabsList>
            ) : null}
            <div className="min-w-0">
              <p className="text-sm font-medium tabular">{t.pageOf(page, total)}</p>
              <p className="text-xs text-muted-foreground tabular">
                {current
                  ? `${m.knowledge.words(current.words)} · ${tokensLabel(current.tokens)}`
                  : " "}
              </p>
            </div>
            <div className="ml-auto flex items-center gap-1">
              <Button
                variant="outline"
                size="icon-sm"
                aria-label={t.previousPage}
                title={t.previousPage}
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                <ChevronLeftIcon />
              </Button>
              <Button
                variant="outline"
                size="icon-sm"
                aria-label={t.nextPage}
                title={t.nextPage}
                disabled={page >= total}
                onClick={() => setPage(page + 1)}
              >
                <ChevronRightIcon />
              </Button>
            </div>
          </div>
          <TabsContent value="text" className="px-5 py-5 md:px-8 md:py-6">
            {pages.isPending ? (
              <div className="max-w-prose space-y-2.5">
                {Array.from({ length: 8 }, (_, i) => (
                  <Skeleton key={i} className="h-4 w-full" />
                ))}
              </div>
            ) : pages.isError ? (
              <ErrorState error={pages.error} onRetry={() => void pages.refetch()} />
            ) : current?.text.trim() ? (
              <div
                className={cn(
                  "max-w-prose break-words whitespace-pre-wrap",
                  doc.format === "md"
                    ? "font-mono text-[13px] leading-6"
                    : "text-[15px] leading-7"
                )}
              >
                {current.text}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{t.emptyPage}</p>
            )}
          </TabsContent>
          {doc.format === "pdf" ? (
            <TabsContent value="pdf">
              <PdfFrame doc={doc} page={page} />
            </TabsContent>
          ) : null}
        </Tabs>
      </div>
    </div>
  )
}

function RangeBar({
  doc,
  page,
  range,
  valid,
  from,
  to,
  onChange,
}: {
  doc: Document
  page: number
  range: Range
  valid: boolean
  from: number | undefined
  to: number | undefined
  onChange: (r: Range) => void
}) {
  const workspace = useWorkspace()
  const [estimate] = useTokenEstimates(workspace.id, [
    valid ? { doc, from, to } : { doc: undefined },
  ])
  const t = m.knowledge.viewer

  return (
    <section
      aria-label={t.range}
      className="flex flex-wrap items-end gap-x-6 gap-y-3 rounded-3xl border px-4 py-3"
    >
      <div className="flex items-end gap-2">
        <div className="grid gap-1.5">
          <Label htmlFor="range-from" className="text-xs text-muted-foreground">
            {t.from}
          </Label>
          <Input
            id="range-from"
            type="number"
            inputMode="numeric"
            min={1}
            max={doc.pageCount}
            className="w-24 tabular"
            value={range.from}
            onChange={(e) => onChange({ ...range, from: e.target.value })}
            aria-invalid={!valid || undefined}
          />
        </div>
        <span className="pb-2 text-muted-foreground">–</span>
        <div className="grid gap-1.5">
          <Label htmlFor="range-to" className="text-xs text-muted-foreground">
            {t.to}
          </Label>
          <Input
            id="range-to"
            type="number"
            inputMode="numeric"
            min={1}
            max={doc.pageCount}
            className="w-24 tabular"
            value={range.to}
            onChange={(e) => onChange({ ...range, to: e.target.value })}
            aria-invalid={!valid || undefined}
          />
        </div>
      </div>
      <div className="flex flex-wrap gap-1.5 pb-0.5">
        <Button
          type="button"
          size="xs"
          variant="outline"
          onClick={() => onChange({ ...range, from: String(page) })}
        >
          {t.startHere(page)}
        </Button>
        <Button
          type="button"
          size="xs"
          variant="outline"
          onClick={() => onChange({ ...range, to: String(page) })}
        >
          {t.endHere(page)}
        </Button>
        <Button
          type="button"
          size="xs"
          variant="ghost"
          onClick={() => onChange({ from: "1", to: String(doc.pageCount) })}
        >
          {t.allPages}
        </Button>
      </div>
      <div className="min-w-48 flex-1 text-right" aria-live="polite">
        {estimate ? (
          <>
            <p className="text-xs text-muted-foreground tabular">
              {t.rangeSummary(estimate.from, estimate.to)} · {m.knowledge.pages(estimate.pages)}
            </p>
            <p className="font-heading text-lg font-semibold tabular">
              {tokensLabel(estimate.tokens, estimate.approximate)}
            </p>
          </>
        ) : (
          <p className="text-sm text-destructive">{t.rangeInvalid(doc.pageCount)}</p>
        )}
      </div>
    </section>
  )
}

function PageNavigator({
  total,
  page,
  chunk,
  pages,
  loading,
  error,
  onRetry,
  inRange,
  onSelect,
}: {
  total: number
  page: number
  chunk: number
  pages: Array<{ number: number; tokens: number; words: number }> | undefined
  loading: boolean
  error: unknown
  onRetry: () => void
  inRange: (n: number) => boolean
  onSelect: (n: number) => void
}) {
  const t = m.knowledge.viewer
  const list = useRef<HTMLUListElement>(null)
  const first = chunk * PAGE_CHUNK + 1
  const last = Math.min(total, first + PAGE_CHUNK - 1)
  const chunks = Math.ceil(total / PAGE_CHUNK)

  // Keep the selected page visible as it changes (buttons, URL), once
  // its chunk has loaded.
  useEffect(() => {
    if (!pages) return
    list.current
      ?.querySelector<HTMLElement>(`[data-page="${page}"]`)
      ?.scrollIntoView({ block: "nearest" })
  }, [page, pages])

  return (
    <nav
      aria-label={t.pagesTitle}
      className="flex flex-col overflow-hidden rounded-3xl border lg:sticky lg:top-4 lg:max-h-[calc(100svh-2rem)]"
    >
      <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
        <span className="text-sm font-medium">{t.pagesTitle}</span>
        {chunks > 1 ? (
          <div className="flex items-center gap-1">
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t.previousChunk}
              title={t.previousChunk}
              disabled={chunk === 0}
              onClick={() => onSelect((chunk - 1) * PAGE_CHUNK + 1)}
            >
              <ChevronLeftIcon />
            </Button>
            <span className="text-xs text-muted-foreground tabular">{t.chunk(first, last)}</span>
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label={t.nextChunk}
              title={t.nextChunk}
              disabled={chunk >= chunks - 1}
              onClick={() => onSelect((chunk + 1) * PAGE_CHUNK + 1)}
            >
              <ChevronRightIcon />
            </Button>
          </div>
        ) : (
          <span className="text-xs text-muted-foreground tabular">{m.knowledge.pages(total)}</span>
        )}
      </div>
      {error ? (
        <div className="p-3">
          <ErrorState error={error} onRetry={onRetry} />
        </div>
      ) : (
        <ul ref={list} className="max-h-72 overflow-y-auto p-1.5 lg:max-h-none lg:flex-1">
          {loading || !pages
            ? Array.from({ length: Math.min(12, last - first + 1) }, (_, i) => (
                <li key={i} className="px-1 py-1">
                  <Skeleton className="h-6 w-full" />
                </li>
              ))
            : pages.map((p) => {
                const selected = p.number === page
                const marked = inRange(p.number)
                return (
                  <li key={p.number}>
                    <button
                      type="button"
                      data-page={p.number}
                      aria-current={selected ? "page" : undefined}
                      aria-label={t.pageButton(p.number, tokensLabel(p.tokens))}
                      title={marked ? t.inRange : undefined}
                      onClick={() => onSelect(p.number)}
                      className={cn(
                        "relative flex w-full items-center gap-2 rounded-xl px-3 py-1.5 text-left text-sm outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/30",
                        selected && "bg-accent text-accent-foreground hover:bg-accent",
                        marked &&
                          "before:absolute before:inset-y-1 before:left-0.5 before:w-0.5 before:rounded-full before:bg-primary/60"
                      )}
                    >
                      <span className="w-12 tabular">{p.number}</span>
                      <span className="ml-auto text-xs text-muted-foreground tabular">
                        {formatCompact(p.tokens)}
                      </span>
                    </button>
                  </li>
                )
              })}
        </ul>
      )}
    </nav>
  )
}

/**
 * The original PDF in the browser's viewer, opened at the selected page.
 * The API forbids framing its responses (X-Frame-Options: DENY), so the
 * frame shows an object URL of the downloaded file instead.
 */
function PdfFrame({ doc, page }: { doc: Document; page: number }) {
  const workspace = useWorkspace()
  const file = useQuery(documentFileQuery(workspace.id, doc.id))

  if (file.isError) {
    return (
      <div className="p-4">
        <ErrorState error={file.error} onRetry={() => void file.refetch()} />
      </div>
    )
  }
  if (!file.data) {
    return (
      <div className="flex h-[calc(100svh-14rem)] min-h-96 items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2Icon className="size-4 animate-spin" />
        {m.knowledge.viewer.loadingPdf}
      </div>
    )
  }
  return <PdfObject blob={file.data} page={page} title={m.knowledge.viewer.pdfTitle(doc.title)} />
}

/**
 * Shows blob through an object URL it owns: made on mount (or for a new
 * blob), revoked on unmount. Chrome's viewer ignores a changed #page
 * fragment, so each page mounts a fresh element (cheap: the bytes are
 * already local). An <embed> rather than an iframe, since Chrome refuses
 * PDFs in sandboxed frames.
 */
function PdfObject({ blob, page, title }: { blob: Blob; page: number; title: string }) {
  const embed = useRef<HTMLEmbedElement>(null)
  const url = useRef<string | null>(null)
  const currentPage = useRef(page)

  useEffect(() => {
    const u = URL.createObjectURL(blob)
    url.current = u
    showPage(embed.current, u, currentPage.current)
    return () => {
      URL.revokeObjectURL(u)
      url.current = null
    }
  }, [blob])

  useEffect(() => {
    currentPage.current = page
    showPage(embed.current, url.current, page)
  }, [page])

  return (
    <embed
      key={page}
      ref={embed}
      type="application/pdf"
      title={title}
      className="block h-[calc(100svh-14rem)] min-h-96 w-full rounded-b-3xl"
    />
  )
}

function showPage(el: HTMLEmbedElement | null, url: string | null, page: number) {
  if (!el || !url) return
  const src = `${url}#page=${page}`
  if (el.getAttribute("src") !== src) el.setAttribute("src", src)
}
