import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import { ArrowLeftIcon } from "lucide-react"
import { toast } from "sonner"

import { ApiError, isApiError, type Pipeline, type PipelineDefinition } from "@/api/client"
import { Banner, EmptyState, ErrorState, PageHeader, ReadOnlyNote } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { pipelineQuery, useCreatePipeline, useRedefinePipeline } from "@/queries/pipelines"
import { PipelineStatusBadge, PipelineSwatch } from "./identity"
import { defaultValues, fromDefinition } from "./model"
import { PipelineForm } from "./pipeline-form"

function BackLink({ workspaceId }: { workspaceId: string }) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className="mb-2 -ml-2"
      render={<Link to="/w/$workspaceId/pipelines" params={{ workspaceId }} />}
      nativeButton={false}
    >
      <ArrowLeftIcon data-icon="inline-start" />
      {m.nav.pipelines}
    </Button>
  )
}

/** The server's rejection, as a banner under the form. */
function ServerError({ error }: { error: unknown }) {
  if (!error) return null
  return (
    <Banner title={m.pipelineForm.serverError}>
      <p>{errorMessage(error)}</p>
    </Banner>
  )
}

export function NewPipelinePage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const create = useCreatePipeline(workspace.id)
  const navigate = useNavigate()
  const [error, setError] = useState<unknown>(null)
  const [initial] = useState(() => defaultValues(workspace.timezone))

  const submit = async (definition: PipelineDefinition) => {
    setError(null)
    try {
      await create.mutateAsync(definition)
      toast.success(m.pipelineForm.created)
      void navigate({ to: "/w/$workspaceId/pipelines", params: { workspaceId: workspace.id } })
    } catch (err) {
      setError(err)
      toast.error(errorMessage(err))
    }
  }

  if (!canEdit) {
    return (
      <div className="max-w-5xl">
        <BackLink workspaceId={workspace.id} />
        <EmptyState title={m.pipelineForm.createForbidden} />
      </div>
    )
  }

  return (
    <div className="max-w-5xl">
      <BackLink workspaceId={workspace.id} />
      <PageHeader title={m.pipelineForm.createTitle} />
      <PipelineForm
        workspaceId={workspace.id}
        initialValues={initial}
        submitLabel={m.pipelineForm.submitCreate}
        banner={<ServerError error={error} />}
        onSubmit={submit}
      />
    </div>
  )
}

export function EditPipelinePage() {
  const workspace = useWorkspace()
  const { pipelineId } = useParams({ from: "/w/$workspaceId/pipelines/$pipelineId" })
  const query = useQuery({
    ...pipelineQuery(workspace.id, pipelineId),
    // The form works from the copy it opened with; a background refetch
    // must not swap it out mid-edit.
    refetchOnWindowFocus: false,
  })

  if (query.isPending) {
    return (
      <div className="max-w-5xl space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-48 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    )
  }
  if (query.isError) {
    if (isApiError(query.error) && (query.error.status === 404 || query.error.status === 400)) {
      return (
        <div className="max-w-5xl">
          <BackLink workspaceId={workspace.id} />
          <EmptyState title={m.pipelineForm.notFound} />
        </div>
      )
    }
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  }
  return <EditPipeline initial={query.data} reload={async () => (await query.refetch()).data} />
}

function EditPipeline({
  initial,
  reload,
}: {
  initial: Pipeline
  reload: () => Promise<Pipeline | undefined>
}) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const navigate = useNavigate()
  // The version (and values) the user started from. PUT sends this
  // version; a newer stored one answers 409 StaleVersion.
  const [base, setBase] = useState(initial)
  const [error, setError] = useState<unknown>(null)
  const [reloading, setReloading] = useState(false)
  const redefine = useRedefinePipeline(workspace.id, base.id)
  const initialValues = useMemo(
    () => fromDefinition(base.definition, workspace.timezone),
    [base, workspace.timezone]
  )

  const submit = async (definition: PipelineDefinition) => {
    setError(null)
    try {
      await redefine.mutateAsync({ version: base.version, definition })
      toast.success(m.pipelineForm.updated)
      void navigate({ to: "/w/$workspaceId/pipelines", params: { workspaceId: workspace.id } })
    } catch (err) {
      setError(err)
      if (!isApiError(err, "StaleVersion")) toast.error(errorMessage(err))
    }
  }

  const doReload = async () => {
    setReloading(true)
    try {
      const fresh = await reload()
      if (fresh) {
        setBase(fresh)
        setError(null)
      }
    } finally {
      setReloading(false)
    }
  }

  const stale = error instanceof ApiError && error.code === "StaleVersion"
  const banner = stale ? (
    <Banner
      tone="warning"
      title={m.pipelineForm.staleTitle}
      action={
        <Button size="sm" variant="outline" disabled={reloading} onClick={() => void doReload()}>
          {m.common.reload}
        </Button>
      }
    >
      <p>{m.pipelineForm.staleBody}</p>
    </Banner>
  ) : (
    <ServerError error={error} />
  )

  return (
    <div className="max-w-5xl">
      <BackLink workspaceId={workspace.id} />
      <PageHeader
        title={canEdit ? m.pipelineForm.editTitle : m.pipelineForm.viewTitle}
        description={
          <span className="flex flex-wrap items-center gap-2">
            <PipelineSwatch id={base.id} />
            <span className="font-medium text-foreground">
              {base.definition.name}
            </span>
            <PipelineStatusBadge status={base.status} />
          </span>
        }
      />
      {canEdit ? null : <ReadOnlyNote>{m.pipelineForm.readOnlyHint}</ReadOnlyNote>}
      <PipelineForm
        // A reload replaces the form's values with the stored version.
        key={base.version}
        workspaceId={workspace.id}
        initialValues={initialValues}
        submitLabel={m.pipelineForm.submitSave}
        banner={banner}
        readOnly={!canEdit}
        onSubmit={submit}
      />
    </div>
  )
}
