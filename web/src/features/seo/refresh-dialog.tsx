import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import { Banner } from "@/components/page"
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
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { toInt } from "@/features/pipelines/model"
import { useCan, useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatInteger } from "@/lib/format"
import { openSeoStatusQuery, seoProfileQuery, useRefreshOpportunities } from "@/queries/seo"
import { CONFIRM_CREDITS, seedList } from "./openseo"

const DEFAULT_COUNT = 20

/**
 * What a refresh will do: `direct` is whether it calls OpenSEO with the
 * instance's API key (undefined when this role cannot tell), and
 * `credits` the most it should spend then (the saved profile's estimate).
 */
export function useRefreshPlan() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const profile = useQuery(seoProfileQuery(workspace.id))
  const status = useQuery({
    ...openSeoStatusQuery(workspace.id),
    enabled: canManage,
  })
  const direct = canManage && status.data ? status.data.configured && !status.data.error : undefined
  const credits = direct === false ? undefined : profile.data?.estimatedCredits || undefined
  return { profile: profile.data, direct, credits }
}

export function RefreshDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        {open ? <RefreshForm onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function RefreshForm({ onDone }: { onDone: () => void }) {
  const workspace = useWorkspace()
  const { profile, direct, credits } = useRefreshPlan()
  const refresh = useRefreshOpportunities(workspace.id)
  const navigate = useNavigate()
  const [count, setCount] = useState(String(DEFAULT_COUNT))
  const [touched, setTouched] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const n = toInt(count)
  const valid = n !== undefined && n >= 1 && n <= 50
  const invalid = touched && !valid
  const jobs = {
    to: "/w/$workspaceId/jobs",
    params: { workspaceId: workspace.id },
  } as const
  const creditsText = credits ? formatInteger(credits) : undefined

  let warning: string | undefined
  if (profile && direct) {
    if (!profile.projectId) warning = m.seo.refreshNoProject
    else if (
      seedList(profile.seeds ?? [], profile.topics).length === 0 &&
      !profile.competitors?.length
    )
      warning = m.seo.refreshNoSeeds
  } else if (profile && !direct && !profile.domain) warning = m.seo.refreshNoDomain

  const queue = async () => {
    if (n === undefined) return
    try {
      await refresh.mutateAsync(n)
      toast.success(m.seo.refreshQueued, {
        description: m.seo.refreshQueuedBody,
        action: { label: m.seo.viewJobs, onClick: () => void navigate(jobs) },
      })
      onDone()
    } catch {
      // shown in the banner
    }
  }

  const submit = () => {
    setTouched(true)
    if (!valid) return
    // A costly refresh is confirmed first.
    if (credits && credits > CONFIRM_CREDITS) setConfirming(true)
    else void queue()
  }

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{m.seo.refreshTitle}</DialogTitle>
        <DialogDescription>
          {direct ? m.seo.refreshDescriptionDirect : m.seo.refreshDescription}{" "}
          <Link
            {...jobs}
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            {m.seo.jobsLink}
          </Link>
          .
        </DialogDescription>
      </DialogHeader>
      {warning ? <Banner tone="warning" title={warning} /> : null}
      <Field data-invalid={invalid || undefined}>
        <FieldLabel htmlFor="seo-refresh-count">{m.seo.refreshCount}</FieldLabel>
        <Input
          id="seo-refresh-count"
          type="number"
          inputMode="numeric"
          min={1}
          max={50}
          step={1}
          className="tabular max-w-32"
          value={count}
          onChange={(e) => setCount(e.target.value)}
          onBlur={() => setTouched(true)}
          aria-invalid={invalid || undefined}
        />
        {invalid ? (
          <FieldError>{m.seo.refreshCountInvalid}</FieldError>
        ) : (
          <FieldDescription>{m.seo.refreshCountHint}</FieldDescription>
        )}
      </Field>
      {creditsText ? (
        <p className="rounded-lg bg-muted/50 px-3 py-2 text-sm">
          {m.seo.refreshEstimate(creditsText)}
        </p>
      ) : null}
      {refresh.error ? <Banner title={errorMessage(refresh.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={refresh.isPending}>
          {creditsText ? m.seo.refreshSubmitCredits(creditsText) : m.seo.refreshSubmit}
        </Button>
      </DialogFooter>
      <AlertDialog open={confirming} onOpenChange={setConfirming}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{m.seo.refreshConfirmTitle(creditsText ?? "")}</AlertDialogTitle>
            <AlertDialogDescription>{m.seo.refreshConfirmBody}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{m.common.cancel}</AlertDialogCancel>
            <AlertDialogAction
              disabled={refresh.isPending}
              onClick={() => {
                setConfirming(false)
                void queue()
              }}
            >
              {m.seo.refreshConfirm}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </form>
  )
}
