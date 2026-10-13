import { useForm } from "@tanstack/react-form"
import { useNavigate, useSearch } from "@tanstack/react-router"

import { isApiError } from "@/api/client"
import { Banner } from "@/components/page"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { FieldGroup } from "@/components/ui/field"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { useLogin } from "@/queries/auth"
import { safeNext } from "./search"
import { GuestPage } from "./standalone-page"
import { TextField } from "./text-field"

function loginErrorMessage(err: unknown): string {
  // Here a 401 is a wrong email or password, not an ended session.
  if (isApiError(err) && err.status === 401) return m.auth.wrongCredentials
  return errorMessage(err)
}

const required = (message: string) => (v: { value: string }) =>
  v.value.trim() ? undefined : message

export function LoginPage() {
  const { next } = useSearch({ from: "/login" })
  const navigate = useNavigate()
  const login = useLogin()
  const form = useForm({
    defaultValues: { email: "", password: "" },
    onSubmit: async ({ value }) => {
      try {
        await login.mutateAsync({ email: value.email.trim(), password: value.password })
      } catch {
        return // shown in the banner
      }
      await navigate({ href: safeNext(next), replace: true })
    },
  })

  return (
    <GuestPage>
      <Card>
        <CardHeader>
          <CardTitle className="font-heading text-lg font-semibold">{m.auth.signInTitle}</CardTitle>
          <CardDescription>{m.auth.signInDescription}</CardDescription>
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
                name="email"
                validators={{
                  onChange: required(m.auth.emailRequired),
                  onSubmit: required(m.auth.emailRequired),
                }}
              >
                {(field) => (
                  <TextField
                    field={field}
                    id="login-email"
                    label={m.auth.email}
                    type="email"
                    autoComplete="username"
                  />
                )}
              </form.Field>
              <form.Field
                name="password"
                validators={{
                  onChange: required(m.auth.passwordRequired),
                  onSubmit: required(m.auth.passwordRequired),
                }}
              >
                {(field) => (
                  <TextField
                    field={field}
                    id="login-password"
                    label={m.auth.password}
                    type="password"
                    autoComplete="current-password"
                  />
                )}
              </form.Field>
            </FieldGroup>
            {login.error ? <Banner title={loginErrorMessage(login.error)} /> : null}
            <form.Subscribe selector={(s) => s.isSubmitting}>
              {(submitting) => (
                <Button type="submit" size="lg" disabled={submitting}>
                  {submitting ? m.auth.signingIn : m.auth.signIn}
                </Button>
              )}
            </form.Subscribe>
          </form>
        </CardContent>
      </Card>
      <p className="px-4 text-center text-xs text-muted-foreground">
        {m.auth.noAccount}{" "}
        <code className="rounded-md bg-muted px-1 py-0.5 font-mono">{m.auth.noAccountCommand}</code>
        .
      </p>
    </GuestPage>
  )
}
