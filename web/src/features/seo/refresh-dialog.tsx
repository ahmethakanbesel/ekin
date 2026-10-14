import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import { Banner } from "@/components/page"
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
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { seoProfileQuery, useRefreshOpportunities } from "@/queries/seo"

const DEFAULT_COUNT = 20

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
  const profile = useQuery(seoProfileQuery(workspace.id))
  const refresh = useRefreshOpportunities(workspace.id)
  const navigate = useNavigate()
  const [count, setCount] = useState(String(DEFAULT_COUNT))
  const [touched, setTouched] = useState(false)
  const n = toInt(count)
  const valid = n !== undefined && n >= 1 && n <= 50
  const invalid = touched && !valid
  const jobs = { to: "/w/$workspaceId/jobs", params: { workspaceId: workspace.id } } as const

  const submit = async () => {
    setTouched(true)
    if (!valid) return
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

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{m.seo.refreshTitle}</DialogTitle>
        <DialogDescription>
          {m.seo.refreshDescription}{" "}
          <Link {...jobs} className="font-medium text-foreground underline-offset-4 hover:underline">
            {m.seo.jobsLink}
          </Link>
          .
        </DialogDescription>
      </DialogHeader>
      {profile.data && !profile.data.domain ? (
        <Banner tone="warning" title={m.seo.refreshNoDomain} />
      ) : null}
      <Field data-invalid={invalid || undefined}>
        <FieldLabel htmlFor="seo-refresh-count">{m.seo.refreshCount}</FieldLabel>
        <Input
          id="seo-refresh-count"
          type="number"
          inputMode="numeric"
          min={1}
          max={50}
          step={1}
          className="max-w-32 tabular"
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
      {refresh.error ? <Banner title={errorMessage(refresh.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={refresh.isPending}>
          {m.seo.refreshSubmit}
        </Button>
      </DialogFooter>
    </form>
  )
}
