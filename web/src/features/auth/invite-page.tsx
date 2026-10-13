import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { Link, useNavigate, useParams } from "@tanstack/react-router"
import { MailXIcon } from "lucide-react"
import { toast } from "sonner"

import { isApiError, type InvitationPreview, type User } from "@/api/client"
import { Banner, EmptyState, ErrorState } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldGroup } from "@/components/ui/field"
import { Skeleton } from "@/components/ui/skeleton"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { browserTimezone, formatDateTime } from "@/lib/time"
import { invitationQuery, meQuery, useAcceptInvitation } from "@/queries/auth"
import { GuestPage } from "./standalone-page"
import { TextField } from "./text-field"
import { confirmError, newPasswordError, personNameError } from "./validation"

const gone = (err: unknown) => isApiError(err) && (err.status === 404 || err.status === 400)

/** /invite/$token: joins a workspace, creating the account if needed. */
export function InvitePage() {
  const { token } = useParams({ from: "/invite/$token" })
  const preview = useQuery(invitationQuery(token))
  // Only to warn about switching accounts; a 401 just means signed out.
  const me = useQuery(meQuery())

  let body
  if (preview.isPending) {
    body = <Skeleton className="h-96 w-full rounded-4xl" />
  } else if (preview.isError) {
    body = gone(preview.error) ? (
      <NotFound signedIn={!!me.data} />
    ) : (
      <ErrorState error={preview.error} onRetry={() => void preview.refetch()} />
    )
  } else {
    body = (
      <AcceptCard token={token} invitation={preview.data} me={me.isError ? undefined : me.data} />
    )
  }

  return <GuestPage>{body}</GuestPage>
}

function NotFound({ signedIn }: { signedIn: boolean }) {
  return (
    <EmptyState
      icon={<MailXIcon />}
      title={m.invite.notFoundTitle}
      body={m.invite.notFoundBody}
      action={
        signedIn ? (
          <Button render={<Link to="/" />} nativeButton={false}>
            {m.errors.goHome}
          </Button>
        ) : (
          <Button render={<Link to="/login" />} nativeButton={false}>
            {m.invite.goToSignIn}
          </Button>
        )
      }
    />
  )
}

function AcceptCard({
  token,
  invitation: inv,
  me,
}: {
  token: string
  invitation: InvitationPreview
  me: User | undefined
}) {
  const accept = useAcceptInvitation(token)
  const navigate = useNavigate()
  const existing = inv.accountExists
  const switching = !!me && me.email.toLowerCase() !== inv.email.toLowerCase()

  const form = useForm({
    defaultValues: { name: "", password: "", confirm: "" },
    onSubmit: async ({ value }) => {
      try {
        const res = await accept.mutateAsync(
          existing
            ? { password: value.password }
            : { name: value.name.trim(), password: value.password },
        )
        toast.success(m.invite.joined(inv.workspaceName))
        void navigate({
          to: "/w/$workspaceId",
          params: { workspaceId: res.workspaceId },
          replace: true,
        })
      } catch {
        // shown in the banner
      }
    },
  })

  const failure = accept.error
  let banner = null
  if (failure) {
    if (isApiError(failure) && failure.status === 401)
      banner = <Banner title={m.invite.wrongPassword} />
    else if (gone(failure))
      banner = <Banner title={m.invite.notFoundTitle}>{m.invite.notFoundBody}</Banner>
    else banner = <Banner title={errorMessage(failure)} />
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-semibold">
          {m.invite.title(inv.workspaceName)}
        </CardTitle>
        <CardDescription>{m.invite.description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-6">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
          <dt className="text-muted-foreground">{m.invite.invitedEmail}</dt>
          <dd className="min-w-0 truncate font-medium">{inv.email}</dd>
          <dt className="text-muted-foreground">{m.invite.role}</dt>
          <dd className="flex flex-wrap items-center gap-2">
            <Badge variant="secondary">{m.roles[inv.role]}</Badge>
            <span className="text-xs text-muted-foreground">{m.roleHints[inv.role]}</span>
          </dd>
          <dt className="text-muted-foreground">{m.invite.expires}</dt>
          <dd className="tabular">{formatDateTime(inv.expiresAt, browserTimezone())}</dd>
        </dl>

        {switching ? (
          <Banner tone="warning" title={m.invite.switchNote(me.email, inv.email)} />
        ) : null}

        <form
          noValidate
          className="grid gap-6"
          onSubmit={(e) => {
            e.preventDefault()
            void form.handleSubmit()
          }}
        >
          <p className="text-sm text-muted-foreground">
            {existing ? m.invite.existingAccount : m.invite.newAccount}
          </p>
          {/* Lets password managers file the new password under the right account. */}
          <input
            type="email"
            name="email"
            value={inv.email}
            autoComplete="username"
            readOnly
            hidden
          />
          <FieldGroup className="gap-5">
            {existing ? null : (
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
                    id="invite-name"
                    label={m.invite.name}
                    autoComplete="name"
                  />
                )}
              </form.Field>
            )}
            <form.Field
              name="password"
              validators={{
                onChange: ({ value }) =>
                  existing
                    ? value
                      ? undefined
                      : m.auth.passwordRequired
                    : newPasswordError(value),
                onSubmit: ({ value }) =>
                  existing
                    ? value
                      ? undefined
                      : m.auth.passwordRequired
                    : newPasswordError(value),
              }}
            >
              {(field) => (
                <TextField
                  field={field}
                  id="invite-password"
                  label={m.invite.password}
                  type="password"
                  autoComplete={existing ? "current-password" : "new-password"}
                  hint={existing ? undefined : m.account.newPasswordHint}
                />
              )}
            </form.Field>
            {existing ? null : (
              <form.Field
                name="confirm"
                validators={{
                  onChangeListenTo: ["password"],
                  onChange: ({ value, fieldApi }) =>
                    confirmError(fieldApi.form.getFieldValue("password"), value),
                  onSubmit: ({ value, fieldApi }) =>
                    confirmError(fieldApi.form.getFieldValue("password"), value),
                }}
              >
                {(field) => (
                  <TextField
                    field={field}
                    id="invite-confirm"
                    label={m.invite.confirmPassword}
                    type="password"
                    autoComplete="new-password"
                  />
                )}
              </form.Field>
            )}
          </FieldGroup>
          {banner}
          <form.Subscribe selector={(s) => s.isSubmitting}>
            {(submitting) => (
              <Button type="submit" size="lg" disabled={submitting}>
                {submitting
                  ? m.invite.joining
                  : existing
                    ? m.invite.signInToJoin
                    : m.invite.createAndJoin}
              </Button>
            )}
          </form.Subscribe>
        </form>
      </CardContent>
    </Card>
  )
}
