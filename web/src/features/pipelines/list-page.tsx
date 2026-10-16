import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import {
  FileTextIcon,
  MoreHorizontalIcon,
  PauseIcon,
  PencilIcon,
  PlayIcon,
  PlusIcon,
  Trash2Icon,
  VideoIcon,
  WorkflowIcon,
} from "lucide-react"
import { toast } from "sonner"

import type { Pipeline } from "@/api/client"
import { EmptyState, ErrorState, PageHeader, TableSkeleton } from "@/components/page"
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
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
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
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { useRetained } from "@/lib/use-retained"
import { cn } from "@/lib/utils"
import {
  pipelinesQuery,
  useDeletePipeline,
  usePausePipeline,
  useResumePipeline,
} from "@/queries/pipelines"
import { ComingWeek } from "./coming-week"
import { PipelineStatusBadge, PipelineSwatch } from "./identity"
import { RunPipelineDialog } from "./run-dialog"
import { outputLines, scheduleText } from "./summary"

export function PipelinesPage() {
  const workspace = useWorkspace()
  const canEdit = useCan("edit")
  const list = useQuery(pipelinesQuery(workspace.id))
  const now = useNow()
  const [running, setRunning] = useState<Pipeline | null>(null)
  const [deleting, setDeleting] = useState<Pipeline | null>(null)

  const newButton = canEdit ? (
    <Button
      render={<Link to="/w/$workspaceId/pipelines/new" params={{ workspaceId: workspace.id }} />}
      nativeButton={false}
    >
      <PlusIcon data-icon="inline-start" />
      {m.pipelines.new}
    </Button>
  ) : null

  let body
  if (list.isPending) body = <TableSkeleton />
  else if (list.isError) body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  else if (list.data.length === 0)
    body = (
      <EmptyState
        icon={<WorkflowIcon />}
        title={m.pipelines.emptyTitle}
        body={m.pipelines.emptyBody}
        action={newButton}
      />
    )
  else
    body = (
      <div className="rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">{m.pipelines.columns.name}</TableHead>
              <TableHead>{m.pipelines.columns.outputs}</TableHead>
              <TableHead>{m.pipelines.columns.source}</TableHead>
              <TableHead>{m.pipelines.columns.schedule}</TableHead>
              <TableHead>{m.pipelines.columns.status}</TableHead>
              <TableHead>{m.pipelines.columns.nextRun}</TableHead>
              <TableHead className="w-12 pr-4">
                <span className="sr-only">{m.common.actions}</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {list.data.map((p) => (
              <PipelineRow
                key={p.id}
                pipeline={p}
                timezone={workspace.timezone}
                now={now}
                canEdit={canEdit}
                onRun={() => setRunning(p)}
                onDelete={() => setDeleting(p)}
              />
            ))}
          </TableBody>
        </Table>
      </div>
    )

  return (
    <>
      <PageHeader
        title={m.pipelines.title}
        description={m.pipelines.description}
        actions={list.data?.length ? newButton : null}
      />
      {body}
      {list.data?.length ? (
        <ComingWeek
          workspaceId={workspace.id}
          pipelines={list.data}
          timezone={workspace.timezone}
          now={now}
        />
      ) : null}
      <RunPipelineDialog pipeline={running} onOpenChange={(open) => !open && setRunning(null)} />
      <DeletePipelineDialog pipeline={deleting} onOpenChange={(open) => !open && setDeleting(null)} />
    </>
  )
}

function PipelineRow({
  pipeline: p,
  timezone,
  now,
  canEdit,
  onRun,
  onDelete,
}: {
  pipeline: Pipeline
  timezone: string
  now: number
  canEdit: boolean
  onRun: () => void
  onDelete: () => void
}) {
  const navigate = useNavigate()
  const pause = usePausePipeline(p.workspaceId)
  const resume = useResumePipeline(p.workspaceId)
  const def = p.definition
  const schedule = def.schedule
  const toggling = pause.isPending || resume.isPending

  const togglePause = async () => {
    try {
      if (p.status === "active") {
        await pause.mutateAsync(p.id)
        toast.success(m.pipelines.paused(def.name))
      } else {
        await resume.mutateAsync(p.id)
        toast.success(m.pipelines.resumed(def.name))
      }
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  const edit = () =>
    void navigate({
      to: "/w/$workspaceId/pipelines/$pipelineId",
      params: { workspaceId: p.workspaceId, pipelineId: p.id },
    })

  return (
    <TableRow>
      <TableCell className="max-w-64 min-w-40 pl-4 align-top whitespace-normal">
        <div className="flex flex-col gap-1">
          <span className="flex items-start gap-2">
            <PipelineSwatch id={p.id} className="mt-1.25" />
            <Link
              to="/w/$workspaceId/pipelines/$pipelineId"
              params={{ workspaceId: p.workspaceId, pipelineId: p.id }}
              className="font-medium hover:underline"
            >
              {def.name}
            </Link>
          </span>
          <span className="pl-4.5 text-xs text-muted-foreground">
            {m.values.language[def.language] ?? def.language}
          </span>
        </div>
      </TableCell>
      <TableCell className="max-w-64 min-w-44 align-top whitespace-normal">
        <ul className="grid gap-1">
          {outputLines(def).map((line) => (
            <li key={line.kind} className="flex items-start gap-1.5 text-sm">
              {line.kind === "video" ? (
                <VideoIcon className="mt-0.75 size-3.5 shrink-0 text-muted-foreground" />
              ) : (
                <FileTextIcon className="mt-0.75 size-3.5 shrink-0 text-muted-foreground" />
              )}
              <span>{line.text}</span>
              {line.lead ? (
                <Badge variant="outline" className="shrink-0">
                  {m.pipelines.lead}
                </Badge>
              ) : null}
            </li>
          ))}
        </ul>
      </TableCell>
      <TableCell className="max-w-40 align-top text-sm whitespace-normal">
        {m.pipelineForm.sourceKinds[def.source.kind]}
        {def.source.kind === "knowledge" && def.source.refs?.length ? (
          <span className="text-muted-foreground"> · {def.source.refs.length}</span>
        ) : null}
      </TableCell>
      <TableCell className="max-w-44 min-w-32 align-top whitespace-normal">
        {schedule ? (
          <ScheduleCell schedule={schedule} />
        ) : (
          <span className="text-sm text-muted-foreground">{m.common.manual}</span>
        )}
      </TableCell>
      <TableCell className="align-top">
        <PipelineStatusBadge status={p.status} />
      </TableCell>
      <TableCell className="align-top">
        {p.nextRunAt ? (
          <div className="flex flex-col gap-0.5">
            <span className="text-sm tabular">{formatDateTime(p.nextRunAt, timezone)}</span>
            <span className="text-xs text-muted-foreground">{formatRelative(p.nextRunAt, now)}</span>
          </div>
        ) : (
          // Paused is said once, in the Status column.
          <span className="text-sm text-muted-foreground">{m.common.none}</span>
        )}
      </TableCell>
      <TableCell className="pr-4 align-top">
        {canEdit ? (
          <div className="flex items-center justify-end gap-1">
            <Button variant="outline" size="sm" onClick={onRun}>
              <PlayIcon data-icon="inline-start" />
              {m.pipelines.runNow}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              disabled={toggling}
              aria-label={`${p.status === "active" ? m.pipelines.pause : m.pipelines.resume}: ${def.name}`}
              title={p.status === "active" ? m.pipelines.pause : m.pipelines.resume}
              onClick={() => void togglePause()}
            >
              {p.status === "active" ? <PauseIcon /> : <PlayIcon />}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button variant="ghost" size="icon-sm" aria-label={m.common.actions} />}
              >
                <MoreHorizontalIcon />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuItem onClick={edit}>
                  <PencilIcon />
                  {m.common.edit}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" onClick={onDelete}>
                  <Trash2Icon />
                  {m.common.delete}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

/** The schedule in words first, the cron expression under it. */
function ScheduleCell({ schedule }: { schedule: NonNullable<Pipeline["definition"]["schedule"]> }) {
  const text = scheduleText(schedule.cron)
  return (
    <div className="flex flex-col gap-0.5">
      {text ? <span className="text-sm">{text}</span> : null}
      <code className={cn("w-fit font-mono", text ? "text-xs text-muted-foreground" : "text-sm")}>
        {schedule.cron}
      </code>
      <span className="text-xs text-muted-foreground">{schedule.timezone}</span>
      <span className="text-xs text-muted-foreground">{m.pipelines.batch(schedule.batch)}</span>
    </div>
  )
}

function DeletePipelineDialog({
  pipeline,
  onOpenChange,
}: {
  pipeline: Pipeline | null
  onOpenChange: (open: boolean) => void
}) {
  const target = useRetained(pipeline)
  const del = useDeletePipeline(target?.workspaceId ?? "")
  const name = target?.definition.name ?? ""

  const confirm = async () => {
    if (!target) return
    try {
      await del.mutateAsync(target.id)
      toast.success(m.pipelines.deleted(name))
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <AlertDialog open={!!pipeline} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.pipelines.deleteTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.pipelines.deleteBody(name)}</AlertDialogDescription>
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
