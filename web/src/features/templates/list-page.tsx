import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useSearch } from "@tanstack/react-router"
import { Loader2Icon, PaletteIcon, PlusIcon, SparklesIcon } from "lucide-react"

import type { Template } from "@/api/client"
import { EmptyState, ErrorState, PageHeader, ReadOnlyNote } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { templatesQuery } from "@/queries/templates"
import { useDraftWatches } from "./draft-watch"
import { GenerateTemplateDialog } from "./generate-dialog"
import { TEMPLATE_TABS, type TemplateTab } from "./model"
import { KindBadges, PaletteStrip, SourceBadge, TitleCard } from "./preview"

const ROUTE = "/w/$workspaceId/templates"

export function TemplatesPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const tab: TemplateTab = search.kind ?? "all"
  const list = useQuery(templatesQuery(workspace.id))
  const drafts = useDraftWatches(workspace.id)
  const [generating, setGenerating] = useState(false)

  const shown = list.data?.filter((t) => tab === "all" || t.kinds.includes(tab))

  let body
  if (list.isPending)
    body = (
      <div className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3" aria-busy="true" aria-label={m.common.loading}>
        {Array.from({ length: 6 }, (_, i) => (
          <Skeleton key={i} className="aspect-[4/5] w-full rounded-xl" />
        ))}
      </div>
    )
  else if (list.isError) body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (!shown || shown.length === 0) {
    const empty = m.templates.empty[tab]
    body = <EmptyState icon={<PaletteIcon />} title={empty.title} body={empty.body} />
  } else
    body = (
      <ul className="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
        {shown.map((t) => (
          <li key={t.id} className="min-w-0">
            <TemplateCard workspaceId={workspace.id} template={t} />
          </li>
        ))}
      </ul>
    )

  return (
    <>
      <PageHeader
        title={m.templates.title}
        description={m.templates.description}
        actions={
          canEdit ? (
            <>
              <Button variant="outline" onClick={() => setGenerating(true)}>
                <SparklesIcon data-icon="inline-start" />
                {m.templates.draft}
              </Button>
              <Button
                render={
                  <Link to="/w/$workspaceId/templates/new" params={{ workspaceId: workspace.id }} />
                }
                nativeButton={false}
              >
                <PlusIcon data-icon="inline-start" />
                {m.templates.new}
              </Button>
            </>
          ) : null
        }
      />
      {canEdit ? null : <ReadOnlyNote>{m.templates.readOnlyHint}</ReadOnlyNote>}
      {drafts.map((d) => (
        <div
          key={d.jobId}
          role="status"
          className="mb-4 flex items-start gap-2.5 rounded-lg border border-sky-500/30 bg-sky-500/5 px-4 py-3 text-sm"
        >
          <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin text-sky-600 dark:text-sky-400" />
          <div className="min-w-0 space-y-0.5">
            <p className="font-medium break-words">{m.templates.generate.drafting(d.channel)}</p>
            <p className="text-muted-foreground">{m.templates.generate.draftingBody}</p>
          </div>
        </div>
      ))}
      <Tabs
        value={tab}
        onValueChange={(next) =>
          void navigate({
            search: next === "all" ? {} : { kind: next as Exclude<TemplateTab, "all"> },
            replace: true,
          })
        }
        className="mb-5"
      >
        <TabsList aria-label={m.templates.kinds}>
          {TEMPLATE_TABS.map((t) => (
            <TabsTrigger key={t} value={t} className="flex-none">
              {m.templates.tabs[t]}
            </TabsTrigger>
          ))}
        </TabsList>
      </Tabs>
      {body}
      <GenerateTemplateDialog
        open={generating}
        onOpenChange={setGenerating}
        defaultKind={tab === "article" ? "article" : "video"}
      />
    </>
  )
}

function TemplateCard({ workspaceId, template: t }: { workspaceId: string; template: Template }) {
  return (
    <Link
      to="/w/$workspaceId/templates/$templateId"
      params={{ workspaceId, templateId: t.id }}
      className="group/template flex h-full flex-col overflow-hidden rounded-xl bg-card text-card-foreground shadow-md ring-1 ring-foreground/5 transition-shadow outline-none hover:shadow-lg hover:ring-foreground/15 focus-visible:ring-[3px] focus-visible:ring-ring dark:ring-foreground/10"
    >
      <TitleCard template={t} />
      <div className="flex flex-1 flex-col gap-3 p-5">
        <PaletteStrip palette={t.palette} />
        <div className="space-y-1">
          <h2 className="font-heading font-medium group-hover/template:underline group-hover/template:underline-offset-4">
            {t.name}
          </h2>
          {t.description ? (
            <p className="line-clamp-3 text-sm text-muted-foreground">
              {t.description}
            </p>
          ) : null}
        </div>
        <div className="mt-auto flex flex-wrap items-center gap-1.5 pt-1">
          <KindBadges kinds={t.kinds} />
          <SourceBadge source={t.source} />
        </div>
        {t.inspiration ? (
          <p className="text-xs text-muted-foreground/80" title={m.templates.inspiration}>
            {t.inspiration}
          </p>
        ) : null}
      </div>
    </Link>
  )
}
