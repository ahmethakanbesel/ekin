import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { ListChecksIcon, XIcon } from "lucide-react"
import { toast } from "sonner"

import type { Job, JobStatus } from "@/api/client"
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
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, formatRelative, useNow } from "@/lib/time"
import { useRetained } from "@/lib/use-retained"
import { cn } from "@/lib/utils"
import { JOB_LIST_LIMIT, jobsQuery, useCancelJob } from "@/queries/jobs"
import { JOB_STATUSES, parseStatusFilter, serializeStatusFilter } from "./search"

const statusTone: Record<JobStatus, string> = {
  queued: "bg-secondary text-secondary-foreground",
  running: "bg-sky-500/15 text-sky-700 dark:text-sky-300",
  succeeded: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300",
  failed: "bg-destructive/10 text-destructive",
  cancelled: "bg-muted text-muted-foreground",
}

export function JobsPage() {
  const workspace = useWorkspace()
  const search = useSearch({ from: "/w/$workspaceId/jobs" })
  const navigate = useNavigate({ from: "/w/$workspaceId/jobs" })
  const status = parseStatusFilter(search.status)
  const jobs = useQuery({
    ...jobsQuery(workspace.id, status),
    refetchInterval: 5_000,
    refetchIntervalInBackground: false,
  })
  const now = useNow(5_000)
  const [cancelling, setCancelling] = useState<Job | null>(null)

  const setStatus = (next: JobStatus[]) =>
    void navigate({ search: { status: serializeStatusFilter(next) }, replace: true })

  const toggle = (s: JobStatus) =>
    setStatus(status.includes(s) ? status.filter((x) => x !== s) : [...status, s])

  let body
  if (jobs.isPending) body = <TableSkeleton rows={6} />
  else if (jobs.isError) body = <ErrorState error={jobs.error} onRetry={() => void jobs.refetch()} />
  else if (jobs.data.length === 0)
    body = (
      <EmptyState
        icon={<ListChecksIcon />}
        title={m.jobs.emptyTitle}
        body={status.length ? m.jobs.emptyFiltered : m.jobs.emptyBody}
      />
    )
  else
    body = (
      <>
        <div className="rounded-3xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">{m.jobs.columns.kind}</TableHead>
                <TableHead>{m.jobs.columns.status}</TableHead>
                <TableHead>{m.jobs.columns.queue}</TableHead>
                <TableHead className="text-right">{m.jobs.columns.attempt}</TableHead>
                <TableHead>{m.jobs.columns.runAfter}</TableHead>
                <TableHead>{m.jobs.columns.lastError}</TableHead>
                <TableHead>{m.jobs.columns.created}</TableHead>
                <TableHead>{m.jobs.columns.updated}</TableHead>
                <TableHead className="w-12 pr-4">
                  <span className="sr-only">{m.common.actions}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {jobs.data.map((job) => (
                <JobRow
                  key={job.id}
                  job={job}
                  timezone={workspace.timezone}
                  now={now}
                  onCancel={() => setCancelling(job)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
        {jobs.data.length >= JOB_LIST_LIMIT ? (
          <p className="mt-3 text-xs text-muted-foreground">{m.jobs.showing(JOB_LIST_LIMIT)}</p>
        ) : null}
      </>
    )

  return (
    <>
      <PageHeader title={m.jobs.title} description={m.jobs.description} />
      <div className="mb-4 flex flex-wrap gap-1.5" role="group" aria-label={m.jobs.columns.status}>
        <Chip active={status.length === 0} onClick={() => setStatus([])}>
          {m.jobs.all}
        </Chip>
        {JOB_STATUSES.map((s) => (
          <Chip key={s} active={status.includes(s)} onClick={() => toggle(s)}>
            {m.jobs.status[s]}
          </Chip>
        ))}
      </div>
      {body}
      <CancelJobDialog job={cancelling} onOpenChange={(open) => !open && setCancelling(null)} />
    </>
  )
}

function Chip({
  active,
  onClick,
  children,
}: {
  active: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? "default" : "outline"}
      aria-pressed={active}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

function JobRow({
  job,
  timezone,
  now,
  onCancel,
}: {
  job: Job
  timezone: string
  now: number
  onCancel: () => void
}) {
  const cancellable = job.status === "queued" || job.status === "running"
  return (
    <TableRow>
      <TableCell className="pl-4 align-top">
        <div className="flex flex-col gap-0.5">
          <span className="font-mono text-xs">{job.kind}</span>
          <span className="font-mono text-[11px] text-muted-foreground" title={job.id}>
            {job.id.slice(0, 8)}
          </span>
        </div>
      </TableCell>
      <TableCell className="align-top">
        <Badge className={cn(statusTone[job.status])}>{m.jobs.status[job.status]}</Badge>
      </TableCell>
      <TableCell className="align-top font-mono text-xs">{job.queue}</TableCell>
      <TableCell className="text-right align-top tabular">
        {job.attempt}/{job.maxAttempts}
      </TableCell>
      <TableCell className="align-top">
        <Stamp iso={job.runAfter} timezone={timezone} now={now} />
      </TableCell>
      <TableCell className="max-w-64 align-top">
        {job.lastError ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="line-clamp-2 cursor-default text-xs break-words text-destructive" />
              }
            >
              {job.lastError}
            </TooltipTrigger>
            <TooltipContent className="max-w-md whitespace-pre-wrap">{job.lastError}</TooltipContent>
          </Tooltip>
        ) : (
          <span className="text-muted-foreground">{m.common.none}</span>
        )}
      </TableCell>
      <TableCell className="align-top">
        <Stamp iso={job.createdAt} timezone={timezone} now={now} />
      </TableCell>
      <TableCell className="align-top">
        <Stamp iso={job.finishedAt ?? job.updatedAt} timezone={timezone} now={now} />
        {job.finishedAt ? (
          <span className="block text-[11px] text-muted-foreground">{m.jobs.columns.finished}</span>
        ) : null}
      </TableCell>
      <TableCell className="pr-4 align-top">
        {cancellable ? (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={m.jobs.cancel}
            title={m.jobs.cancel}
            onClick={onCancel}
          >
            <XIcon />
          </Button>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

function Stamp({ iso, timezone, now }: { iso: string; timezone: string; now: number }) {
  return (
    <div className="flex flex-col gap-0.5 whitespace-nowrap">
      <span className="text-xs tabular">{formatDateTime(iso, timezone)}</span>
      <span className="text-[11px] text-muted-foreground">{formatRelative(iso, now)}</span>
    </div>
  )
}

function CancelJobDialog({
  job,
  onOpenChange,
}: {
  job: Job | null
  onOpenChange: (open: boolean) => void
}) {
  const target = useRetained(job)
  const cancel = useCancelJob(target?.workspaceId ?? "")

  const confirm = async () => {
    if (!target) return
    try {
      await cancel.mutateAsync(target.id)
      toast.success(m.jobs.cancelled)
      onOpenChange(false)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <AlertDialog open={!!job} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{m.jobs.cancelTitle}</AlertDialogTitle>
          <AlertDialogDescription>{m.jobs.cancelBody(target?.kind ?? "")}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{m.jobs.keep}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={cancel.isPending}
            onClick={() => void confirm()}
          >
            {m.jobs.cancelConfirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
