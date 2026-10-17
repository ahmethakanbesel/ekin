import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { toast } from "sonner"

import type { WorkspaceProduction } from "@/api/client"
import { Banner, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  fromProduction,
  toProduction,
  toSelfReview,
} from "@/features/pipelines/model"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { pipelinesQuery } from "@/queries/pipelines"
import { useSaveWorkspaceProduction, workspaceProductionQuery } from "@/queries/production"
import { TemplateSelect } from "@/features/templates/template-select"
import { LayeredCard, LayeredGroup, LayeredRow } from "./layered"
import { ProductionFields, changedLabels, countSet, serverResolved } from "./production-fields"
import { SettingsSectionHeader } from "./settings-page"

/**
 * Settings › Production: the workspace's defaults for how stories are
 * made, each beside what the server gives. Every pipeline and request
 * that leaves a setting unset follows these.
 */
export function ProductionSection() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const query = useQuery(workspaceProductionQuery(workspace.id))

  let body
  if (query.isPending)
    body = (
      <div className="grid gap-3 rounded-xl p-6 ring-1 ring-foreground/10" aria-busy="true" aria-label={m.common.loading}>
        <Skeleton className="h-5 w-2/3" />
        {Array.from({ length: 8 }, (_, i) => (
          <div key={i} className="grid grid-cols-[10rem_1fr_16rem] items-center gap-5 py-2">
            <Skeleton className="h-4 w-28" />
            <Skeleton className="h-4 w-36" />
            <Skeleton className="h-9 w-full" />
          </div>
        ))}
      </div>
    )
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  else
    body = (
      // Re-mount the form when the stored defaults change underneath it.
      <ProductionForm
        key={JSON.stringify([query.data.defaults, query.data.selfReviewRounds, query.data.templateId])}
        stored={query.data}
        readOnly={!canManage}
      />
    )

  return (
    <div className="max-w-5xl">
      <SettingsSectionHeader
        section="production"
        readOnly={canManage ? undefined : m.workspace.readOnlyHint}
      />
      {body}
    </div>
  )
}

function ProductionForm({ stored, readOnly }: { stored: WorkspaceProduction; readOnly: boolean }) {
  const workspace = useWorkspace()
  const save = useSaveWorkspaceProduction(workspace.id)
  const [initial] = useState(() => fromProduction(stored.defaults, stored.selfReviewRounds))
  const [value, setValue] = useState(initial)
  const savedTemplate = stored.templateId ?? ""
  const [templateId, setTemplateId] = useState(savedTemplate)
  const t = m.settings.production
  // The summary counts what is saved; the save bar names what is not.
  const counted = countSet(initial, { video: true, longVideo: true })
  const set = counted.set + (savedTemplate ? 1 : 0)
  const total = counted.total + 1
  const changed = [
    ...(templateId !== savedTemplate ? [t.defaultTemplate] : []),
    ...changedLabels(value, initial),
  ]
  const dirty = changed.length > 0
  const pipelines = useQuery(pipelinesQuery(workspace.id))
  const overriding = (pipelines.data ?? []).filter(
    (p) =>
      p.definition.production !== undefined ||
      p.definition.selfReviewRounds !== undefined ||
      p.definition.templateId !== undefined ||
      !!p.definition.noTemplate
  ).length

  const submit = async () => {
    try {
      await save.mutateAsync({
        defaults: toProduction(value) ?? {},
        selfReviewRounds: toSelfReview(value),
        templateId: templateId || undefined,
      })
      toast.success(t.saved)
    } catch {
      // shown in the banner
    }
  }

  return (
    <form
      noValidate
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <LayeredCard
        description={t.serverNote(stored.server.profile)}
        summary={
          <>
            <span className="font-medium text-foreground tabular">
              {m.settings.setCount(set, total, workspace.name)}
            </span>
            {pipelines.data ? (
              <>
                {" · "}
                <Link
                  to="/w/$workspaceId/pipelines"
                  params={{ workspaceId: workspace.id }}
                  className="underline decoration-input underline-offset-4 hover:text-foreground"
                >
                  {overriding > 0 ? t.reach(overriding) : t.allFollow}
                </Link>
              </>
            ) : null}
          </>
        }
        fromLabel={m.settings.columns.inherited}
        hereLabel={workspace.name}
        // Unclipped, so the save bar can stick to the bottom of the screen.
        className="overflow-visible"
        footer={
          readOnly ? null : (
            <div
              className={cn(
                "flex flex-wrap items-center justify-between gap-3 rounded-b-xl border-t px-6 py-4",
                dirty && "sticky bottom-0 z-10 bg-card/95 backdrop-blur"
              )}
            >
              <p className="min-w-0 text-sm text-muted-foreground tabular" aria-live="polite">
                {dirty ? t.unsavedNamed(changed.length, changed.join(", ")) : t.allSaved}
              </p>
              <div className="flex gap-2">
                {dirty ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setValue(initial)
                      setTemplateId(savedTemplate)
                    }}
                  >
                    {m.settings.discard}
                  </Button>
                ) : null}
                <Button
                  type="submit"
                  variant={dirty ? "default" : "outline"}
                  disabled={!dirty || save.isPending}
                >
                  {save.isPending ? m.common.saving : t.save}
                </Button>
              </div>
            </div>
          )
        }
      >
        <div className="pb-4">
          <LayeredGroup title={t.groups.style}>
            <LayeredRow
              label={t.defaultTemplate}
              htmlFor="ws-production-template"
              hint={t.defaultTemplateHint}
              inherited={{ value: m.templates.picker.none, source: m.settings.layers.builtIn }}
              set={templateId !== ""}
              dirty={templateId !== savedTemplate}
              readOnly={readOnly}
              onReset={() => setTemplateId("")}
            >
              {(describedBy) => (
                <TemplateSelect
                  id="ws-production-template"
                  value={templateId}
                  onChange={setTemplateId}
                  kinds={["video", "article"]}
                  bare
                  describedBy={describedBy}
                  disabled={readOnly}
                />
              )}
            </LayeredRow>
          </LayeredGroup>
          <ProductionFields
            idPrefix="ws-production"
            value={value}
            onChange={setValue}
            saved={initial}
            inherited={serverResolved(stored.server)}
            here={workspace.name}
            video
            longVideo
            readOnly={readOnly}
          />
        </div>
        {save.error ? (
          <div className="px-6 pb-4">
            <Banner title={errorMessage(save.error)} />
          </div>
        ) : null}
      </LayeredCard>
    </form>
  )
}
