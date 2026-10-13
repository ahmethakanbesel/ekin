import { useState } from "react"
import { useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import type { Pipeline } from "@/api/client"
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
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useRunPipeline } from "@/queries/pipelines"
import { toInt } from "./model"

export function RunPipelineDialog({
  pipeline,
  onOpenChange,
}: {
  pipeline: Pipeline | null
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={!!pipeline} onOpenChange={onOpenChange}>
      <DialogContent>
        {pipeline ? <RunForm pipeline={pipeline} onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function RunForm({ pipeline, onDone }: { pipeline: Pipeline; onDone: () => void }) {
  const [count, setCount] = useState(String(pipeline.definition.schedule?.batch ?? 1))
  const [touched, setTouched] = useState(false)
  const run = useRunPipeline(pipeline.workspaceId)
  const navigate = useNavigate()
  const n = toInt(count)
  const valid = n !== undefined && n >= 1 && n <= 20
  const invalid = touched && !valid

  const submit = async () => {
    setTouched(true)
    if (!valid) return
    try {
      await run.mutateAsync({ pipelineId: pipeline.id, count: n })
      toast.success(m.pipelines.runQueued, {
        action: {
          label: m.pipelines.viewJobs,
          onClick: () =>
            void navigate({
              to: "/w/$workspaceId/jobs",
              params: { workspaceId: pipeline.workspaceId },
            }),
        },
      })
      onDone()
    } catch (err) {
      toast.error(errorMessage(err))
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
        <DialogTitle>{m.pipelines.runTitle}</DialogTitle>
        <DialogDescription>{m.pipelines.runDescription(pipeline.definition.name)}</DialogDescription>
      </DialogHeader>
      <Field data-invalid={invalid || undefined}>
        <FieldLabel htmlFor="run-count">{m.pipelines.runCount}</FieldLabel>
        <Input
          id="run-count"
          type="number"
          inputMode="numeric"
          min={1}
          max={20}
          step={1}
          className="max-w-32"
          value={count}
          onChange={(e) => setCount(e.target.value)}
          onBlur={() => setTouched(true)}
          aria-invalid={invalid || undefined}
        />
        {invalid ? (
          <FieldError>{m.pipelines.runCountInvalid}</FieldError>
        ) : (
          <FieldDescription>{m.pipelines.runCountHint}</FieldDescription>
        )}
      </Field>
      {run.error ? <Banner title={errorMessage(run.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={run.isPending}>
          {m.pipelines.runNow}
        </Button>
      </DialogFooter>
    </form>
  )
}
