import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { toast } from "sonner"

import { isApiError, type User } from "@/api/client"
import { Banner, PageHeader } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { meQuery, useChangePassword, useUpdateMe } from "@/queries/auth"
import { StandalonePage } from "./standalone-page"
import { TextField } from "./text-field"
import { confirmError, newPasswordError, personNameError } from "./validation"

export function AccountPage() {
  const { data: me } = useQuery(meQuery())
  return (
    <StandalonePage>
      <PageHeader title={m.account.title} description={m.account.description} />
      {me ? (
        <div className="grid gap-6">
          {/* Re-mount when the stored name changes underneath the form. */}
          <ProfileCard key={`${me.id}:${me.name}`} me={me} />
          <NotificationsCard me={me} />
          <PasswordCard />
        </div>
      ) : (
        <Skeleton className="h-64 w-full" />
      )}
    </StandalonePage>
  )
}

function ProfileCard({ me }: { me: User }) {
  const update = useUpdateMe()
  const form = useForm({
    defaultValues: { name: me.name },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync({ name: value.name.trim() })
        toast.success(m.account.nameSaved)
      } catch (err) {
        toast.error(errorMessage(err))
      }
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-base font-medium">
          {m.account.profileTitle}
        </CardTitle>
        <CardDescription>{m.account.profileDescription}</CardDescription>
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
              validators={{
                onChange: ({ value }) => personNameError(value),
                onSubmit: ({ value }) => personNameError(value),
              }}
            >
              {(field) => (
                <TextField
                  field={field}
                  id="account-name"
                  label={m.account.name}
                  autoComplete="name"
                />
              )}
            </form.Field>
            <Field>
              <FieldLabel htmlFor="account-email">{m.account.email}</FieldLabel>
              <Input id="account-email" type="email" value={me.email} readOnly disabled />
              <FieldDescription>{m.account.emailHint}</FieldDescription>
            </Field>
          </FieldGroup>
          {update.error ? <Banner title={errorMessage(update.error)} /> : null}
          <div className="flex justify-end">
            <form.Subscribe selector={(s) => [s.isSubmitting, s.isDirty] as const}>
              {([submitting, dirty]) => (
                <Button type="submit" variant={dirty ? "default" : "outline"} disabled={submitting || !dirty}>
                  {submitting ? m.common.saving : m.common.save}
                </Button>
              )}
            </form.Subscribe>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

function NotificationsCard({ me }: { me: User }) {
  const update = useUpdateMe()
  // Shows the requested state while the request is in flight.
  const checked = update.isPending
    ? (update.variables.emailNotifications ?? me.emailNotifications)
    : me.emailNotifications

  const toggle = async (on: boolean) => {
    try {
      await update.mutateAsync({ emailNotifications: on })
      toast.success(on ? m.account.emailDigestsOn : m.account.emailDigestsOff)
    } catch (err) {
      toast.error(errorMessage(err))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-base font-medium">
          {m.account.notificationsTitle}
        </CardTitle>
        <CardDescription>{m.account.notificationsDescription}</CardDescription>
      </CardHeader>
      <CardContent>
        <Field orientation="horizontal">
          <FieldContent>
            <FieldLabel htmlFor="account-email-digests">{m.account.emailDigests}</FieldLabel>
            <FieldDescription>{m.account.emailDigestsHint}</FieldDescription>
          </FieldContent>
          <Switch
            id="account-email-digests"
            checked={checked}
            disabled={update.isPending}
            onCheckedChange={(on) => void toggle(on)}
          />
        </Field>
      </CardContent>
    </Card>
  )
}

function PasswordCard() {
  const change = useChangePassword()
  const form = useForm({
    defaultValues: { currentPassword: "", newPassword: "", confirm: "" },
    onSubmit: async ({ value, formApi }) => {
      try {
        await change.mutateAsync({
          currentPassword: value.currentPassword,
          newPassword: value.newPassword,
        })
        toast.success(m.account.passwordChanged)
        formApi.reset()
        change.reset()
      } catch {
        // shown in the banner
      }
    },
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-base font-medium">
          {m.account.passwordTitle}
        </CardTitle>
        <CardDescription>{m.account.passwordDescription}</CardDescription>
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
              name="currentPassword"
              validators={{
                onSubmit: ({ value }) => (value ? undefined : m.auth.passwordRequired),
              }}
            >
              {(field) => (
                <TextField
                  field={field}
                  id="account-current-password"
                  label={m.account.currentPassword}
                  type="password"
                  autoComplete="current-password"
                />
              )}
            </form.Field>
            <form.Field
              name="newPassword"
              validators={{
                onChange: ({ value }) => newPasswordError(value),
                onSubmit: ({ value }) => newPasswordError(value),
              }}
            >
              {(field) => (
                <TextField
                  field={field}
                  id="account-new-password"
                  label={m.account.newPassword}
                  type="password"
                  autoComplete="new-password"
                  hint={m.account.newPasswordHint}
                />
              )}
            </form.Field>
            <form.Field
              name="confirm"
              validators={{
                onChangeListenTo: ["newPassword"],
                onChange: ({ value, fieldApi }) =>
                  confirmError(fieldApi.form.getFieldValue("newPassword"), value),
                onSubmit: ({ value, fieldApi }) =>
                  confirmError(fieldApi.form.getFieldValue("newPassword"), value),
              }}
            >
              {(field) => (
                <TextField
                  field={field}
                  id="account-confirm-password"
                  label={m.account.confirmPassword}
                  type="password"
                  autoComplete="new-password"
                />
              )}
            </form.Field>
          </FieldGroup>
          {change.error ? (
            <Banner
              title={
                // The new password is checked above, so the server's
                // ValidationFailed is about the current one.
                isApiError(change.error, "ValidationFailed")
                  ? m.account.wrongPassword
                  : errorMessage(change.error)
              }
            />
          ) : null}
          <div className="flex justify-end">
            <form.Subscribe selector={(s) => s.isSubmitting}>
              {(submitting) => (
                <Button type="submit" disabled={submitting}>
                  {submitting ? m.common.saving : m.account.changePassword}
                </Button>
              )}
            </form.Subscribe>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}
