import { useMemo, useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import { toast } from "sonner"

import { isApiError, type Template, type TemplateInput } from "@/api/client"
import { Banner, EmptyState, ErrorState, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { templateQuery, useCreateTemplate, useUpdateTemplate } from "@/queries/templates"
import { BackToTemplates } from "./detail-page"
import { emptyTemplateValues, fromTemplate, isEditable } from "./model"
import { TemplateForm } from "./template-form"

/** The server's rejection, as a banner under the form. */
function ServerError({ error }: { error: unknown }) {
  if (!error) return null
  return (
    <Banner title={m.templates.form.serverError}>
      <p>{errorMessage(error)}</p>
    </Banner>
  )
}

export function NewTemplatePage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const create = useCreateTemplate(workspace.id)
  const navigate = useNavigate()
  const [error, setError] = useState<unknown>(null)
  const [initial] = useState(emptyTemplateValues)

  const submit = async (input: TemplateInput) => {
    setError(null)
    try {
      const created = await create.mutateAsync(input)
      toast.success(m.templates.form.created)
      void navigate({
        to: "/w/$workspaceId/templates/$templateId",
        params: { workspaceId: workspace.id, templateId: created.id },
      })
    } catch (err) {
      setError(err)
      toast.error(errorMessage(err))
    }
  }

  if (!canEdit) {
    return (
      <div className="max-w-6xl">
        <BackToTemplates workspaceId={workspace.id} />
        <EmptyState title={m.templates.form.forbidden} />
      </div>
    )
  }

  return (
    <div className="max-w-6xl">
      <BackToTemplates workspaceId={workspace.id} />
      <PageHeader title={m.templates.form.createTitle} />
      <TemplateForm
        initialValues={initial}
        submitLabel={m.templates.form.submitCreate}
        banner={<ServerError error={error} />}
        cancel={
          <Button
            variant="outline"
            render={<Link to="/w/$workspaceId/templates" params={{ workspaceId: workspace.id }} />}
            nativeButton={false}
          >
            {m.common.cancel}
          </Button>
        }
        onSubmit={submit}
      />
    </div>
  )
}

export function EditTemplatePage() {
  const workspace = useWorkspace()
  const { templateId } = useParams({ from: "/w/$workspaceId/templates/$templateId/edit" })
  const query = useQuery({
    ...templateQuery(workspace.id, templateId),
    // The form works from the copy it opened with.
    refetchOnWindowFocus: false,
  })

  if (query.isPending) {
    return (
      <div className="max-w-6xl space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }
  if (query.isError) {
    if (isApiError(query.error) && (query.error.status === 404 || query.error.status === 400)) {
      return (
        <div className="max-w-6xl">
          <BackToTemplates workspaceId={workspace.id} />
          <EmptyState title={m.templates.notFound} />
        </div>
      )
    }
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  }
  return <EditTemplate template={query.data} reload={async () => (await query.refetch()).data} />
}

function EditTemplate({
  template,
  reload,
}: {
  template: Template
  reload: () => Promise<Template | undefined>
}) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const update = useUpdateTemplate(workspace.id, template.id)
  const navigate = useNavigate()
  const [error, setError] = useState<unknown>(null)
  // The version (and values) the user started from. PUT sends this
  // version; a newer stored one answers 409 StaleVersion.
  const [base, setBase] = useState(template)
  const [reloading, setReloading] = useState(false)
  const initial = useMemo(() => fromTemplate(base), [base])
  const detail = {
    to: "/w/$workspaceId/templates/$templateId",
    params: { workspaceId: workspace.id, templateId: template.id },
  } as const

  if (!canEdit || !isEditable(template)) {
    return (
      <div className="max-w-6xl">
        <BackToTemplates workspaceId={workspace.id} />
        <EmptyState
          title={canEdit ? m.templates.builtinReadOnly : m.templates.form.forbidden}
          action={
            <Button variant="outline" render={<Link {...detail} />} nativeButton={false}>
              {m.common.back}
            </Button>
          }
        />
      </div>
    )
  }

  const submit = async (input: TemplateInput) => {
    setError(null)
    try {
      await update.mutateAsync({ ...input, version: base.version })
      toast.success(m.templates.form.saved)
      void navigate(detail)
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

  const banner = isApiError(error, "StaleVersion") ? (
    <Banner
      tone="warning"
      title={m.templates.form.staleTitle}
      action={
        <Button size="sm" variant="outline" disabled={reloading} onClick={() => void doReload()}>
          {m.common.reload}
        </Button>
      }
    >
      <p>{m.templates.form.staleBody}</p>
    </Banner>
  ) : (
    <ServerError error={error} />
  )

  return (
    <div className="max-w-6xl">
      <BackToTemplates workspaceId={workspace.id} />
      <PageHeader title={m.templates.form.editTitle} description={template.name} />
      <TemplateForm
        key={base.version}
        initialValues={initial}
        submitLabel={m.templates.form.submitSave}
        banner={banner}
        cancel={
          <Button variant="outline" render={<Link {...detail} />} nativeButton={false}>
            {m.common.cancel}
          </Button>
        }
        onSubmit={submit}
      />
    </div>
  )
}
