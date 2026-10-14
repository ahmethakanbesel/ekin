import { useMemo, useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import { ChevronRightIcon, Trash2Icon } from "lucide-react"

import {
  isApiError,
  type Deliverable,
  type KnowledgeRef,
  type Story,
} from "@/api/client"
import {
  EmptyState,
  ErrorState,
  PageHeader,
  ReadOnlyNote,
} from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { cn } from "@/lib/utils"
import { documentsQuery } from "@/queries/documents"
import { pipelinesQuery } from "@/queries/pipelines"
import { storyQuery } from "@/queries/stories"
import { BackToStories } from "./back-link"
import { KindIcon, statusTone } from "./badges"
import { DiscardChangesDialog } from "./discard-dialog"
import { DeliverablePanel } from "./deliverable-panel"
import { deliverableLabel } from "./model"
import { DeleteStoryDialog } from "./review-dialogs"

const ROUTE = "/w/$workspaceId/stories/$storyId"

export function StoryPage() {
  const workspace = useWorkspace()
  const { storyId } = useParams({ from: ROUTE })
  const query = useQuery(storyQuery(workspace.id, storyId))

  if (query.isPending) {
    return (
      <div className="grid gap-4">
        <Skeleton className="h-8 w-2/3" />
        <Skeleton className="h-5 w-1/2" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="aspect-video w-full max-w-3xl" />
      </div>
    )
  }
  if (query.isError) {
    if (
      isApiError(query.error) &&
      (query.error.status === 404 || query.error.status === 400)
    ) {
      return (
        <div className="max-w-5xl">
          <BackToStories workspaceId={workspace.id} />
          <EmptyState title={m.story.notFound} />
        </div>
      )
    }
    return (
      <ErrorState error={query.error} onRetry={() => void query.refetch()} />
    )
  }
  return <StoryView story={query.data} />
}

/** The deliverable to open first: one waiting for review, else the first. */
function firstToOpen(story: Story): Deliverable | undefined {
  return (
    story.deliverables.find((d) => d.status === "review") ??
    story.deliverables[0]
  )
}

function StoryView({ story }: { story: Story }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const canManage = useCan("manage")
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState(false)
  const [picked, setPicked] = useState<string | null>(null)
  // The deliverable whose editor holds unsaved changes, and the tab the
  // reviewer asked for meanwhile (switching tabs unmounts the editor).
  const [dirtyId, setDirtyId] = useState<string | null>(null)
  const [pendingTab, setPendingTab] = useState<string | null>(null)
  const selected =
    (picked && story.deliverables.find((d) => d.id === picked)?.id) ||
    firstToOpen(story)?.id

  return (
    <div className="min-w-0">
      <BackToStories workspaceId={workspace.id} />
      <PageHeader title={story.title} description={story.angle || undefined} />
      <StoryFacts
        story={story}
        workspaceId={workspace.id}
        timezone={workspace.timezone}
        actions={
          canEdit ? (
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive"
              onClick={() => setDeleting(true)}
            >
              <Trash2Icon data-icon="inline-start" />
              {m.story.delete}
            </Button>
          ) : null
        }
      />
      {canEdit ? null : (
        <ReadOnlyNote className="mt-4">{m.story.readOnlyHint}</ReadOnlyNote>
      )}

      {story.deliverables.length === 0 ? (
        <EmptyState className="mt-6" title={m.story.noDeliverables} />
      ) : (
        <Tabs
          value={selected}
          onValueChange={(v) => {
            const next = String(v)
            if (dirtyId && dirtyId !== next) setPendingTab(next)
            else setPicked(next)
          }}
          className="mt-6 gap-5"
        >
          <div className="-mx-1 overflow-x-auto px-1 pb-1">
            <TabsList aria-label={m.story.deliverables}>
              {story.deliverables.map((d) => (
                <TabsTrigger
                  key={d.id}
                  value={d.id}
                  className="flex-none gap-2"
                >
                  <KindIcon kind={d.kind} className="text-current" />
                  {deliverableLabel(d)}
                  <span
                    className={cn(
                      "rounded-full px-1.5 text-[11px] leading-4 font-medium",
                      statusTone[d.status]
                    )}
                  >
                    {m.stories.status[d.status] ?? d.status}
                  </span>
                </TabsTrigger>
              ))}
            </TabsList>
          </div>
          {story.deliverables.map((d) => (
            <TabsContent key={d.id} value={d.id}>
              <DeliverablePanel
                workspaceId={workspace.id}
                timezone={workspace.timezone}
                story={story}
                deliverable={d}
                canEdit={canEdit}
                canManage={canManage}
                onDirtyChange={(dirty) =>
                  setDirtyId((cur) => (dirty ? d.id : cur === d.id ? null : cur))
                }
              />
            </TabsContent>
          ))}
        </Tabs>
      )}

      <DiscardChangesDialog
        open={pendingTab !== null}
        onKeep={() => setPendingTab(null)}
        onDiscard={() => {
          setPicked(pendingTab)
          setPendingTab(null)
        }}
      />
      <DeleteStoryDialog
        workspaceId={workspace.id}
        story={deleting ? story : null}
        onOpenChange={setDeleting}
        onDeleted={() =>
          void navigate({
            to: "/w/$workspaceId/stories",
            params: { workspaceId: workspace.id },
          })
        }
      />
    </div>
  )
}

function StoryFacts({
  story: s,
  workspaceId,
  timezone,
  actions,
}: {
  story: Story
  workspaceId: string
  timezone: string
  /** Shown at the end of the facts row (delete). */
  actions?: ReactNode
}) {
  const now = useNow()
  const pipelines = useQuery({
    ...pipelinesQuery(workspaceId),
    enabled: !!s.pipelineId,
  })
  const pipeline = s.pipelineId
    ? pipelines.data?.find((p) => p.id === s.pipelineId)
    : undefined

  let origin: ReactNode
  if (!s.pipelineId) origin = m.stories.origin.adhoc
  else if (pipeline)
    origin = (
      <>
        <Link
          to="/w/$workspaceId/pipelines/$pipelineId"
          params={{ workspaceId, pipelineId: pipeline.id }}
          className="font-medium hover:underline"
        >
          {pipeline.definition.name}
        </Link>
        <span className="text-muted-foreground">
          {" "}
          · {m.stories.origin[s.origin]}
        </span>
      </>
    )
  else origin = pipelines.data ? m.stories.deletedPipeline : m.common.none

  return (
    <div className="grid gap-3">
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
        <dl className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
          <Fact label={m.story.origin}>{origin}</Fact>
          <Fact label={m.story.language}>
            {m.values.language[s.language] ?? s.language}
          </Fact>
          {s.templateName ? <Fact label={m.story.template}>{s.templateName}</Fact> : null}
          {s.keyword ? (
            <Fact label={m.story.keyword}>
              <span className="font-medium">{s.keyword}</span>
            </Fact>
          ) : null}
          <Fact label={m.story.created}>
            <span
              className="tabular"
              title={formatDateTime(s.createdAt, timezone)}
            >
              {formatDateTime(s.createdAt, timezone)}
            </span>
            <span className="text-muted-foreground">
              {" "}
              · {formatRelative(s.createdAt, now)}
            </span>
          </Fact>
        </dl>
        {actions}
      </div>
      {s.keywords.length > 0 ? (
        <ul className="flex flex-wrap gap-1.5" aria-label={m.story.keywords}>
          {s.keywords.map((k) => (
            <li key={k}>
              <Badge variant="secondary" className="font-normal">
                {k}
              </Badge>
            </li>
          ))}
        </ul>
      ) : null}
      {s.instructions || s.refs?.length ? (
        <div className="grid gap-2 md:grid-cols-2">
          {s.instructions ? (
            <Disclosure title={m.story.instructions}>
              <p className="max-h-80 overflow-y-auto font-mono text-[13px] whitespace-pre-wrap">
                {s.instructions}
              </p>
            </Disclosure>
          ) : null}
          {s.refs?.length ? (
            <Disclosure title={m.story.refs(s.refs.length)}>
              <RefList workspaceId={workspaceId} refs={s.refs} />
            </Disclosure>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd>{children}</dd>
    </div>
  )
}

function Disclosure({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <details className="group self-start rounded-3xl border">
      <summary className="flex cursor-pointer list-none items-center gap-2 px-4 py-2.5 text-sm font-medium select-none [&::-webkit-details-marker]:hidden">
        <ChevronRightIcon className="size-4 text-muted-foreground transition-transform group-open:rotate-90" />
        {title}
      </summary>
      <div className="border-t px-4 py-3">{children}</div>
    </details>
  )
}

function RefList({
  workspaceId,
  refs,
}: {
  workspaceId: string
  refs: KnowledgeRef[]
}) {
  const documents = useQuery(documentsQuery(workspaceId))
  const byId = useMemo(
    () => new Map((documents.data ?? []).map((d) => [d.id.toLowerCase(), d])),
    [documents.data]
  )
  return (
    <ul className="grid gap-1.5 text-sm">
      {refs.map((r, i) => {
        const doc = byId.get(r.documentId.toLowerCase())
        const range =
          r.fromPage && r.toPage
            ? m.knowledge.viewer.rangeSummary(r.fromPage, r.toPage)
            : m.story.wholeDocument
        return (
          <li
            key={`${r.documentId}-${i}`}
            className="flex flex-wrap items-baseline gap-x-2"
          >
            {doc ? (
              <Link
                to="/w/$workspaceId/knowledge/$documentId"
                params={{ workspaceId, documentId: doc.id }}
                search={r.fromPage ? { page: r.fromPage } : {}}
                className="font-medium hover:underline"
              >
                {doc.title}
              </Link>
            ) : (
              <span className="text-muted-foreground">
                {m.pipelineForm.unknownDocument}
              </span>
            )}
            <span className="tabular text-xs text-muted-foreground">
              {range}
            </span>
          </li>
        )
      })}
    </ul>
  )
}
