import { useState, type ReactNode } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import { ArrowLeftIcon, CopyIcon, LockIcon, PencilIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { isApiError, type Template } from "@/api/client"
import { EmptyState, ErrorState, ReadOnlyNote } from "@/components/page"
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
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Markdown } from "@/features/stories/markdown"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { templateQuery, useDeleteTemplate, useDuplicateTemplate } from "@/queries/templates"
import { bodyStyle, headingStyle, isEditable } from "./model"
import { KindBadges, PaletteChips, SourceBadge, TitleCard } from "./preview"

const ROUTE = "/w/$workspaceId/templates/$templateId"

export function BackToTemplates({ workspaceId }: { workspaceId: string }) {
  return (
    <Button
      variant="ghost"
      size="sm"
      className="mb-2 -ml-2"
      render={<Link to="/w/$workspaceId/templates" params={{ workspaceId }} />}
      nativeButton={false}
    >
      <ArrowLeftIcon data-icon="inline-start" />
      {m.nav.templates}
    </Button>
  )
}

export function TemplatePage() {
  const workspace = useWorkspace()
  const { templateId } = useParams({ from: ROUTE })
  const query = useQuery(templateQuery(workspace.id, templateId))

  if (query.isPending) {
    return (
      <div className="grid gap-6">
        <Skeleton className="h-8 w-40" />
        <div className="grid gap-6 lg:grid-cols-[minmax(0,28rem)_1fr]">
          <Skeleton className="aspect-video w-full rounded-xl" />
          <Skeleton className="h-40 w-full" />
        </div>
        <Skeleton className="h-96 w-full rounded-xl" />
      </div>
    )
  }
  if (query.isError) {
    if (isApiError(query.error) && (query.error.status === 404 || query.error.status === 400)) {
      return (
        <div className="max-w-5xl">
          <BackToTemplates workspaceId={workspace.id} />
          <EmptyState title={m.templates.notFound} />
        </div>
      )
    }
    return <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  }
  return <TemplateView template={query.data} />
}

function TemplateView({ template: t }: { template: Template }) {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const navigate = useNavigate()
  const now = useNow()
  const duplicate = useDuplicateTemplate(workspace.id)
  const [deleting, setDeleting] = useState(false)
  const editable = isEditable(t)

  const copy = async () => {
    try {
      const created = await duplicate.mutateAsync(t.id)
      toast.success(m.templates.duplicated(created.name))
      void navigate({
        to: "/w/$workspaceId/templates/$templateId",
        params: { workspaceId: workspace.id, templateId: created.id },
      })
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <div className="min-w-0">
      <BackToTemplates workspaceId={workspace.id} />
      <div className="mb-8 grid items-start gap-6 lg:grid-cols-[minmax(0,30rem)_1fr] lg:gap-10">
        <div className="overflow-hidden rounded-xl shadow-md ring-1 ring-foreground/10">
          <TitleCard template={t} size="lg" />
        </div>
        <div className="grid min-w-0 gap-4">
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-1.5">
              <KindBadges kinds={t.kinds} />
              <SourceBadge source={t.source} />
            </div>
            <h1 className="font-heading text-2xl font-semibold tracking-tight break-words">{t.name}</h1>
            {t.description ? <p className="text-muted-foreground">{t.description}</p> : null}
          </div>
          <dl className="grid gap-x-8 gap-y-2 text-sm sm:grid-cols-[auto_1fr]">
            {t.inspiration ? (
              <Fact label={m.templates.inspiration}>{t.inspiration}</Fact>
            ) : null}
            {editable ? (
              <Fact label={m.templates.updated}>
                <span title={formatDateTime(t.updatedAt, workspace.timezone)}>
                  {/* A server clock slightly ahead must not read “in 3 seconds”. */}
                  {formatRelative(t.updatedAt, Math.max(now, Date.parse(t.updatedAt)))}
                </span>
              </Fact>
            ) : null}
          </dl>
          {canEdit ? (
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="outline" disabled={duplicate.isPending} onClick={() => void copy()}>
                <CopyIcon data-icon="inline-start" />
                {m.templates.duplicate}
              </Button>
              {editable ? (
                <>
                  <Button
                    variant="outline"
                    render={
                      <Link
                        to="/w/$workspaceId/templates/$templateId/edit"
                        params={{ workspaceId: workspace.id, templateId: t.id }}
                      />
                    }
                    nativeButton={false}
                  >
                    <PencilIcon data-icon="inline-start" />
                    {m.common.edit}
                  </Button>
                  <Button
                    variant="ghost"
                    className="text-destructive"
                    onClick={() => setDeleting(true)}
                  >
                    <Trash2Icon data-icon="inline-start" />
                    {m.common.delete}
                  </Button>
                </>
              ) : null}
            </div>
          ) : null}
          {canEdit && !editable ? (
            <p className="flex items-center gap-2 text-sm text-muted-foreground">
              <LockIcon className="size-4 shrink-0" />
              {m.templates.builtinReadOnly}
            </p>
          ) : null}
          {canEdit ? null : <ReadOnlyNote className="mb-0">{m.templates.readOnlyHint}</ReadOnlyNote>}
        </div>
      </div>

      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Card className="min-w-0 xl:order-1">
          <CardHeader>
            <CardTitle>{m.templates.guide}</CardTitle>
          </CardHeader>
          <CardContent>
            <Markdown source={t.guide} className="max-w-[72ch] text-[15px]" />
          </CardContent>
        </Card>
        <div className="grid gap-6 xl:order-2">
          <Card size="sm">
            <CardHeader>
              <CardTitle>{m.templates.palette}</CardTitle>
            </CardHeader>
            <CardContent>
              <PaletteChips palette={t.palette} />
            </CardContent>
          </Card>
          <Card size="sm">
            <CardHeader>
              <CardTitle>{m.templates.fonts}</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4">
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">
                  {m.templates.headingFont} · {t.headingFont}
                </p>
                <p className="text-2xl leading-tight" style={headingStyle(t.headingFont)}>
                  {t.name}
                </p>
              </div>
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">
                  {m.templates.bodyFont} · {t.bodyFont}
                </p>
                <p className="text-[15px]" style={bodyStyle(t.bodyFont)}>
                  {m.templates.sampleBody}
                </p>
              </div>
            </CardContent>
          </Card>
          {t.kinds.includes("video") ? (
            <Card size="sm">
              <CardHeader>
                <CardTitle>{m.templates.musicMood}</CardTitle>
              </CardHeader>
              <CardContent>
                <p className={t.musicMood ? undefined : "text-muted-foreground"}>
                  {t.musicMood || m.templates.noMusicMood}
                </p>
              </CardContent>
            </Card>
          ) : null}
        </div>
      </div>

      <DeleteTemplateDialog
        workspaceId={workspace.id}
        template={deleting ? t : null}
        onOpenChange={setDeleting}
        onDeleted={() =>
          void navigate({ to: "/w/$workspaceId/templates", params: { workspaceId: workspace.id } })
        }
      />
    </div>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-words">{children}</dd>
    </>
  )
}

function DeleteTemplateDialog({
  workspaceId,
  template,
  onOpenChange,
  onDeleted,
}: {
  workspaceId: string
  template: Template | null
  onOpenChange: (open: boolean) => void
  onDeleted: () => void
}) {
  const del = useDeleteTemplate(workspaceId)
  const name = template?.name ?? ""

  const confirm = async () => {
    if (!template) return
    try {
      await del.mutateAsync(template.id)
      toast.success(m.templates.deleted(name))
      onOpenChange(false)
      onDeleted()
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <AlertDialog open={!!template} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.templates.deleteTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.templates.deleteBody(name)}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={del.isPending}
            onClick={() => void confirm()}
          >
            {m.common.delete}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
