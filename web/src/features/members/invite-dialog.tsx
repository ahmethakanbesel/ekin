import { useRef, useState } from "react"
import { useForm } from "@tanstack/react-form"
import { CheckIcon, CopyIcon } from "lucide-react"
import { toast } from "sonner"

import type { Role } from "@/api/client"
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
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { useWorkspace } from "@/features/workspaces/context"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { formatDateTime } from "@/lib/time"
import { useCreateInvitation } from "@/queries/members"
import { isRole, roleItems } from "./roles"

type Created = { email: string; url: string; expiresAt: string; emailed: boolean }

export function InviteDialog({
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
        {open ? <InviteFlow onDone={() => onOpenChange(false)} /> : null}
      </DialogContent>
    </Dialog>
  )
}

/** First the form; once created, the link — the only time it is shown. */
function InviteFlow({ onDone }: { onDone: () => void }) {
  const [created, setCreated] = useState<Created | null>(null)
  return created ? (
    <InviteLink created={created} onDone={onDone} />
  ) : (
    <InviteForm onCreated={setCreated} onCancel={onDone} />
  )
}

const emailError = (email: string) =>
  /^[^\s@]+@[^\s@]+$/.test(email.trim()) ? undefined : m.members.emailInvalid

function InviteForm({
  onCreated,
  onCancel,
}: {
  onCreated: (created: Created) => void
  onCancel: () => void
}) {
  const workspace = useWorkspace()
  const create = useCreateInvitation(workspace.id)
  const form = useForm({
    defaultValues: { email: "", role: "editor" as Role },
    onSubmit: async ({ value }) => {
      try {
        const res = await create.mutateAsync({ email: value.email.trim(), role: value.role })
        onCreated({
          email: res.invitation.email,
          // The server may answer with a path; a link to send needs the origin.
          url: new URL(res.url, window.location.origin).href,
          expiresAt: res.invitation.expiresAt,
          emailed: res.emailed ?? false,
        })
      } catch {
        // shown in the banner
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
        <DialogTitle>{m.members.inviteTitle}</DialogTitle>
        <DialogDescription>{m.members.inviteDescription}</DialogDescription>
      </DialogHeader>
      <FieldGroup className="gap-5">
        <form.Field
          name="email"
          validators={{
            onChange: ({ value }) => emailError(value),
            onSubmit: ({ value }) => emailError(value),
          }}
        >
          {(field) => {
            const invalid = field.state.meta.isTouched && !field.state.meta.isValid
            return (
              <Field data-invalid={invalid || undefined}>
                <FieldLabel htmlFor="invite-email">{m.members.email}</FieldLabel>
                <Input
                  id="invite-email"
                  type="email"
                  autoComplete="off"
                  placeholder={m.members.emailPlaceholder}
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
        <form.Field name="role">
          {(field) => (
            <Field>
              <FieldLabel htmlFor="invite-role">{m.members.role}</FieldLabel>
              <Select
                items={roleItems}
                value={field.state.value}
                onValueChange={(v) => isRole(v) && field.handleChange(v)}
              >
                <SelectTrigger id="invite-role" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {roleItems.map((it) => (
                    <SelectItem key={it.value} value={it.value}>
                      {it.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <FieldDescription>{m.roleHints[field.state.value]}</FieldDescription>
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      {create.error ? <Banner title={errorMessage(create.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          {m.common.cancel}
        </Button>
        <form.Subscribe selector={(s) => s.isSubmitting}>
          {(submitting) => (
            <Button type="submit" disabled={submitting}>
              {submitting ? m.common.creating : m.members.createInvite}
            </Button>
          )}
        </form.Subscribe>
      </DialogFooter>
    </form>
  )
}

function InviteLink({ created, onDone }: { created: Created; onDone: () => void }) {
  const workspace = useWorkspace()
  const input = useRef<HTMLInputElement>(null)
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(created.url)
      setCopied(true)
      toast.success(m.members.copied)
    } catch {
      // No clipboard access (an insecure origin, a denied permission):
      // leave the link selected for a manual copy.
      input.current?.select()
      toast.error(m.members.copyFailed)
    }
  }

  return (
    <div className="grid gap-6">
      <DialogHeader>
        <DialogTitle>{m.members.linkTitle}</DialogTitle>
        <DialogDescription>
          {created.emailed
            ? m.members.linkEmailed(created.email)
            : m.members.linkDescription(created.email)}
        </DialogDescription>
      </DialogHeader>
      <Field>
        <FieldLabel htmlFor="invite-link">{m.members.link}</FieldLabel>
        <InputGroup>
          <InputGroupInput
            ref={input}
            id="invite-link"
            readOnly
            value={created.url}
            className="font-mono text-xs"
            onFocus={(e) => e.currentTarget.select()}
          />
          <InputGroupAddon align="inline-end">
            <InputGroupButton onClick={() => void copy()}>
              {copied ? <CheckIcon /> : <CopyIcon />}
              {m.members.copy}
            </InputGroupButton>
          </InputGroupAddon>
        </InputGroup>
        <FieldDescription>
          {m.members.linkExpires(formatDateTime(created.expiresAt, workspace.timezone))}
        </FieldDescription>
      </Field>
      <DialogFooter>
        <Button onClick={onDone}>{m.members.done}</Button>
      </DialogFooter>
    </div>
  )
}
