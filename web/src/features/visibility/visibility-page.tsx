import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Loader2Icon, PlayIcon } from "lucide-react"
import { toast } from "sonner"

import type { VisibilityOverview } from "@/api/client"
import { Banner, ErrorState, PageHeader, TableSkeleton } from "@/components/page"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { runActive, useStartRun, visibilityQuery } from "@/queries/visibility"
import { PromptsPanel } from "./prompts-panel"
import { Results } from "./results"
import { SettingsForm } from "./settings-form"

const t = m.visibility
type View = "results" | "prompts" | "settings"

/** AI visibility: whether answer engines name the brand for the questions buyers ask. */
export function VisibilityPage() {
  const workspace = useWorkspace()
  const query = useQuery(visibilityQuery(workspace.id))
  const o = query.data
  const [view, setView] = useState<View>()
  const shown: View = view ?? (o?.settings ? (o.runs.length ? "results" : "prompts") : "settings")

  let body
  if (query.isPending) body = <TableSkeleton rows={5} />
  else if (query.isError) body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  else if (o)
    body = (
      <div className="grid gap-6">
        {o.ready ? null : (
          <Banner
            tone="warning"
            title={t.notReadyTitle}
            action={
              <Button size="sm" variant="outline" render={<Link to="/system" />} nativeButton={false}>
                {m.youtube.systemLink}
              </Button>
            }
          >
            <p>{t.notReadyBody}</p>
          </Banner>
        )}
        <RunStatus overview={o} />
        <Tabs value={shown} onValueChange={(v) => setView(v as View)}>
          <TabsList aria-label={t.title}>
            <TabsTrigger value="results" disabled={!o.settings}>
              {t.tabs.results}
            </TabsTrigger>
            <TabsTrigger value="prompts">{t.tabs.prompts(o.prompts.length)}</TabsTrigger>
            <TabsTrigger value="settings">{t.tabs.settings}</TabsTrigger>
          </TabsList>
        </Tabs>
        {shown === "results" ? (
          <Results prompts={o.prompts} version={o.runs[0] ? `${o.runs[0].id}:${o.runs[0].status}` : ""} />
        ) : shown === "prompts" ? (
          <PromptsPanel prompts={o.prompts} setUp={!!o.settings} />
        ) : (
          <SettingsForm
            key={JSON.stringify(o.settings ?? o.suggested)}
            initial={o.settings ?? o.suggested}
            onSaved={() => !o.settings && setView("prompts")}
          />
        )}
      </div>
    )

  return (
    <>
      <PageHeader title={t.title} description={t.description} actions={o?.settings ? <RunButton overview={o} /> : null} />
      {body}
    </>
  )
}

function RunStatus({ overview: o }: { overview: VisibilityOverview }) {
  const latest = o.runs[0]
  if (!latest || !runActive(o)) return null
  return (
    <div className="flex items-center gap-2 rounded-xl border bg-info/5 px-4 py-3 text-sm" role="status">
      <Loader2Icon className="size-4 animate-spin text-info" />
      {latest.status === "queued" ? t.queued : t.running(latest.done + latest.failed, latest.planned)}
    </div>
  )
}

function RunButton({ overview: o }: { overview: VisibilityOverview }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const start = useStartRun(workspace.id)
  const [confirming, setConfirming] = useState(false)
  if (!canEdit) return null
  const e = o.estimate
  const left = Math.max(e.cap - e.used, 0)
  const run = async () => {
    try {
      await start.mutateAsync()
      toast.success(t.started)
      setConfirming(false)
    } catch {
      // shown in the dialog
    }
  }
  return (
    <>
      <Button disabled={runActive(o) || !o.ready || e.calls === 0} onClick={() => setConfirming(true)}>
        <PlayIcon data-icon="inline-start" />
        {t.run}
      </Button>
      <AlertDialog
        open={confirming}
        onOpenChange={(next) => {
          if (!next) start.reset()
          setConfirming(next)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t.confirmTitle(e.calls)}</AlertDialogTitle>
            <AlertDialogDescription>{t.confirmBody(e.prompts, e.models, e.used, e.cap)}</AlertDialogDescription>
          </AlertDialogHeader>
          {e.calls > left ? <Banner title={t.overCap(left)} /> : null}
          {start.error ? <Banner title={errorMessage(start.error)} /> : null}
          <AlertDialogFooter>
            <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
            <AlertDialogAction disabled={start.isPending || e.calls > left} onClick={() => void run()}>
              {t.runCalls(e.calls)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
