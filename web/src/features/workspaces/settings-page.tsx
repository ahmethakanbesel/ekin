import { useCallback } from "react"
import { useForm } from "@tanstack/react-form"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { toast } from "sonner"

import { Banner, PageHeader, ReadOnlyNote } from "@/components/page"
import { TimezoneCombobox } from "@/components/timezone-combobox"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { PublishingSection } from "@/features/publishing/publishing-section"
import { useYouTubeReturn } from "@/features/publishing/youtube-return"
import { VoiceSection } from "@/features/voice/voice-section"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime, useNow } from "@/lib/time"
import { useUpdateWorkspace } from "@/queries/workspaces"
import { useCan, useWorkspace } from "./context"
import { workspaceNameError } from "./validation"

const ROUTE = "/w/$workspaceId/settings"

export function WorkspaceSettingsPage() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const search = useSearch({ from: ROUTE })
  const navigate = useNavigate({ from: ROUTE })
  const clear = useCallback(() => void navigate({ search: {}, replace: true }), [navigate])
  useYouTubeReturn(search, clear, workspace.id)

  return (
    <div className="max-w-3xl">
      <PageHeader title={m.workspace.settingsTitle} description={m.workspace.settingsDescription} />
      {canManage ? null : <ReadOnlyNote>{m.workspace.readOnlyHint}</ReadOnlyNote>}
      {/* Re-mount the form when the stored workspace changes underneath it. */}
      <SettingsForm key={`${workspace.id}:${workspace.updatedAt}`} />
      <VoiceSection />
      <PublishingSection />
    </div>
  )
}

function SettingsForm() {
  const workspace = useWorkspace()
  const canManage = useCan("manage")
  const update = useUpdateWorkspace(workspace.id)
  const now = useNow()
  const form = useForm({
    defaultValues: { name: workspace.name, timezone: workspace.timezone },
    onSubmit: async ({ value }) => {
      const patch: { name?: string; timezone?: string } = {}
      if (value.name.trim() !== workspace.name) patch.name = value.name.trim()
      if (value.timezone !== workspace.timezone) patch.timezone = value.timezone
      try {
        await update.mutateAsync(patch)
        toast.success(m.workspace.updated)
      } catch (err) {
        toast.error(errorMessage(err))
      }
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>{m.workspace.generalTitle}</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          noValidate
          className="grid gap-6"
          onSubmit={(e) => {
            e.preventDefault()
            void form.handleSubmit()
          }}
        >
          <FieldGroup className="gap-5">
            <form.Field
              name="name"
              validators={{ onChange: ({ value }) => workspaceNameError(value) }}
            >
              {(field) => {
                const invalid = field.state.meta.isTouched && !field.state.meta.isValid
                return (
                  <Field data-invalid={invalid || undefined}>
                    <FieldLabel htmlFor="ws-settings-name">{m.workspace.name}</FieldLabel>
                    <Input
                      id="ws-settings-name"
                      autoComplete="off"
                      disabled={!canManage}
                      value={field.state.value}
                      onChange={(e) => field.handleChange(e.target.value)}
                      onBlur={field.handleBlur}
                      aria-invalid={invalid || undefined}
                    />
                    {invalid ? (
                      <FieldError>{String(field.state.meta.errors[0])}</FieldError>
                    ) : null}
                  </Field>
                )
              }}
            </form.Field>
            <form.Field name="timezone">
              {(field) => (
                <Field>
                  <FieldLabel htmlFor="ws-settings-tz">{m.workspace.timezone}</FieldLabel>
                  <TimezoneCombobox
                    id="ws-settings-tz"
                    value={field.state.value}
                    onChange={field.handleChange}
                    disabled={!canManage}
                    className="max-w-sm"
                  />
                  <FieldDescription>
                    {formatDateTime(new Date(now).toISOString(), field.state.value)}
                  </FieldDescription>
                </Field>
              )}
            </form.Field>
          </FieldGroup>
          {update.error ? <Banner title={errorMessage(update.error)} /> : null}
          {canManage ? (
            <div className="flex justify-end">
              <form.Subscribe selector={(s) => [s.isSubmitting, s.isDirty] as const}>
                {([submitting, dirty]) => (
                  <Button
                    type="submit"
                    variant={dirty ? "default" : "outline"}
                    disabled={submitting || !dirty}
                  >
                    {submitting ? m.common.saving : m.common.save}
                  </Button>
                )}
              </form.Subscribe>
            </div>
          ) : null}
        </form>
      </CardContent>
    </Card>
  )
}
