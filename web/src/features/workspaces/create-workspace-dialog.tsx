import { useForm } from "@tanstack/react-form"
import { useNavigate } from "@tanstack/react-router"
import { toast } from "sonner"

import { TimezoneCombobox } from "@/components/timezone-combobox"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Banner } from "@/components/page"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { browserTimezone } from "@/lib/time"
import { useCreateWorkspace } from "@/queries/workspaces"
import { workspaceNameError } from "./validation"

export function CreateWorkspaceDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        {/* Mounted only while open, so each opening starts blank. */}
        {open ? <CreateWorkspaceForm onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

function CreateWorkspaceForm({ onDone }: { onDone: () => void }) {
  const create = useCreateWorkspace()
  const navigate = useNavigate()
  const form = useForm({
    defaultValues: { name: "", timezone: browserTimezone() },
    onSubmit: async ({ value }) => {
      try {
        const ws = await create.mutateAsync({ name: value.name.trim(), timezone: value.timezone })
        toast.success(m.workspace.created(ws.name))
        onDone()
        void navigate({ to: "/w/$workspaceId/pipelines", params: { workspaceId: ws.id } })
      } catch (err) {
        toast.error(errorMessage(err))
      }
    },
  })

  return (
    <form
      noValidate
      className="grid gap-6"
      onSubmit={(e) => {
        e.preventDefault()
        void form.handleSubmit()
      }}
    >
      <DialogHeader>
        <DialogTitle>{m.workspace.createTitle}</DialogTitle>
        <DialogDescription>{m.workspace.createDescription}</DialogDescription>
      </DialogHeader>
      <FieldGroup className="gap-5">
        <form.Field
          name="name"
          validators={{ onChange: ({ value }) => workspaceNameError(value), onSubmit: ({ value }) => workspaceNameError(value) }}
        >
          {(field) => {
            const invalid = field.state.meta.isTouched && !field.state.meta.isValid
            return (
              <Field data-invalid={invalid || undefined}>
                <FieldLabel htmlFor="ws-name">{m.workspace.name}</FieldLabel>
                <Input
                  id="ws-name"
                  autoComplete="off"
                  placeholder={m.workspace.namePlaceholder}
                  value={field.state.value}
                  onChange={(e) => field.handleChange(e.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={invalid || undefined}
                />
                {invalid ? <FieldError>{String(field.state.meta.errors[0])}</FieldError> : null}
              </Field>
            )
          }}
        </form.Field>
        <form.Field name="timezone">
          {(field) => (
            <Field>
              <FieldLabel htmlFor="ws-timezone">{m.workspace.timezone}</FieldLabel>
              <TimezoneCombobox
                id="ws-timezone"
                value={field.state.value}
                onChange={field.handleChange}
              />
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      {create.error ? <Banner title={errorMessage(create.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <form.Subscribe selector={(s) => s.isSubmitting}>
          {(submitting) => (
            <Button type="submit" disabled={submitting}>
              {submitting ? m.common.creating : m.common.create}
            </Button>
          )}
        </form.Subscribe>
      </DialogFooter>
    </form>
  )
}
