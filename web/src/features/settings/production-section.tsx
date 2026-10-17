import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import type { WorkspaceProduction } from "@/api/client"
import { Banner, ErrorState } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  fromProduction,
  toProduction,
  toSelfReview,
  type ProductionValues,
} from "@/features/pipelines/model"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useSaveWorkspaceProduction, workspaceProductionQuery } from "@/queries/production"
import { LayeredCard } from "./layered"
import { ProductionFields, countSet, serverResolved } from "./production-fields"
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
  if (query.isPending) body = <Skeleton className="h-[34rem] w-full rounded-xl" />
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  else
    body = (
      // Re-mount the form when the stored defaults change underneath it.
      <ProductionForm
        key={JSON.stringify([query.data.defaults, query.data.selfReviewRounds])}
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

const flat = (v: ProductionValues) => ({ ...v, ...v.media, media: undefined })

/** How many settings differ between two sets of values. */
function changes(a: ProductionValues, b: ProductionValues): number {
  const fa = flat(a)
  const fb = flat(b)
  return (Object.keys(fa) as (keyof typeof fa)[]).filter((k) => fa[k] !== fb[k]).length
}

function ProductionForm({ stored, readOnly }: { stored: WorkspaceProduction; readOnly: boolean }) {
  const workspace = useWorkspace()
  const save = useSaveWorkspaceProduction(workspace.id)
  const [initial] = useState(() => fromProduction(stored.defaults, stored.selfReviewRounds))
  const [value, setValue] = useState(initial)
  const t = m.settings.production
  const { set, total } = countSet(value, { video: true, longVideo: true })
  const changed = changes(value, initial)

  const submit = async () => {
    try {
      await save.mutateAsync({ defaults: toProduction(value) ?? {}, selfReviewRounds: toSelfReview(value) })
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
          </>
        }
        fromLabel={m.settings.columns.inherited}
        hereLabel={workspace.name}
        footer={
          readOnly ? null : (
            <div className="flex flex-wrap items-center justify-between gap-3 border-t px-6 py-4">
              <p className="text-sm text-muted-foreground tabular" aria-live="polite">
                {changed > 0 ? m.settings.unsaved(changed) : t.allSaved}
              </p>
              <div className="flex gap-2">
                {changed > 0 ? (
                  <Button type="button" variant="outline" onClick={() => setValue(initial)}>
                    {m.settings.discard}
                  </Button>
                ) : null}
                <Button
                  type="submit"
                  variant={changed > 0 ? "default" : "outline"}
                  disabled={changed === 0 || save.isPending}
                >
                  {save.isPending ? m.common.saving : t.save}
                </Button>
              </div>
            </div>
          )
        }
      >
        <div className="pb-4">
          <ProductionFields
            idPrefix="ws-production"
            value={value}
            onChange={setValue}
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
