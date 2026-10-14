import { useState } from "react"
import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { PlusIcon } from "lucide-react"
import { toast } from "sonner"

import type { HostProfile } from "@/api/client"
import { Banner, ErrorState } from "@/components/page"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Skeleton } from "@/components/ui/skeleton"
import { Switch } from "@/components/ui/switch"
import { Row, shownError } from "@/features/pipelines/form-parts"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { profilesQuery, useSaveProfile } from "@/queries/system"

export const QUEUES = ["control", "agent", "tts", "render", "extract", "io"] as const
const PROFILE_NAME = /^[a-z0-9][a-z0-9-]{0,31}$/

/** Admins: how hard the workers using each profile may work. */
export function HostProfiles() {
  const query = useQuery(profilesQuery())
  const [creating, setCreating] = useState(false)

  let body
  if (query.isPending) body = <Skeleton className="h-96 w-full rounded-4xl" />
  else if (query.isError)
    body = <ErrorState error={query.error} onRetry={() => void query.refetch()} />
  else {
    const { profiles, current } = query.data
    // The server's own profile first, the rest by name.
    const ordered = profiles.toSorted((a, b) =>
      a.name === current ? -1 : b.name === current ? 1 : a.name.localeCompare(b.name)
    )
    body = (
      <div className="grid gap-6">
        {ordered.map((p) => (
          // Re-mount the form when the stored profile changes underneath it.
          <ProfileCard key={JSON.stringify(p)} profile={p} current={p.name === current} />
        ))}
      </div>
    )
  }

  const template =
    query.data?.profiles.find((p) => p.name === query.data.current) ?? query.data?.profiles[0]

  return (
    <section>
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div className="space-y-1">
          <h2 className="font-heading text-lg font-semibold tracking-tight">
            {m.system.profilesTitle}
          </h2>
          <p className="text-sm text-muted-foreground">{m.system.profilesDescription}</p>
        </div>
        {query.data ? (
          <Button variant="outline" onClick={() => setCreating(true)}>
            <PlusIcon data-icon="inline-start" />
            {m.system.newProfile}
          </Button>
        ) : null}
      </div>
      {body}
      {query.data && template ? (
        <NewProfileDialog
          open={creating}
          onOpenChange={setCreating}
          template={template}
          taken={query.data.profiles.map((p) => p.name)}
        />
      ) : null}
    </section>
  )
}

type Values = {
  concurrency: Record<string, string>
  heavyExclusive: boolean
  paused: boolean
  cooldownSeconds: string
  renderDuty: string
  renderChunks: string
  previewScale: string
  finalScale: string
}

function toValues(p: HostProfile): Values {
  const concurrency: Record<string, string> = {}
  for (const q of QUEUES) concurrency[q] = String(p.concurrency[q] ?? 0)
  return {
    concurrency,
    heavyExclusive: p.heavyExclusive,
    paused: p.paused,
    cooldownSeconds: String(p.cooldownSeconds),
    renderDuty: String(p.renderDuty),
    renderChunks: String(p.renderChunks),
    previewScale: String(p.previewScale),
    finalScale: String(p.finalScale),
  }
}

function toProfile(name: string, v: Values, base: HostProfile): HostProfile {
  // Queues this UI does not know about keep their stored value.
  const concurrency: Record<string, number> = { ...base.concurrency }
  for (const q of QUEUES) concurrency[q] = Number(v.concurrency[q])
  return {
    name,
    concurrency,
    heavyExclusive: v.heavyExclusive,
    paused: v.paused,
    cooldownSeconds: Number(v.cooldownSeconds),
    renderDuty: Number(v.renderDuty),
    renderChunks: Number(v.renderChunks),
    previewScale: Number(v.previewScale),
    finalScale: Number(v.finalScale),
  }
}

const wholeIn = (min: number, max: number) => (s: string) => {
  const t = s.trim()
  return /^\d+$/.test(t) && Number(t) >= min && Number(t) <= max
    ? undefined
    : m.system.wholeRange(min, max)
}

const numberIn = (min: number, max: number) => (s: string) => {
  const t = s.trim()
  return /^\d*\.?\d+$/.test(t) && Number(t) >= min && Number(t) <= max
    ? undefined
    : m.system.range(min, max)
}

const queueError = wholeIn(0, 8)

const NUMBER_FIELDS = [
  {
    name: "cooldownSeconds",
    label: m.system.cooldown,
    hint: m.system.cooldownHint,
    check: wholeIn(0, 3600),
    step: 1,
  },
  {
    name: "renderDuty",
    label: m.system.renderDuty,
    hint: m.system.renderDutyHint,
    check: numberIn(0.05, 1),
    step: 0.05,
  },
  {
    name: "renderChunks",
    label: m.system.renderChunks,
    hint: m.system.renderChunksHint,
    check: wholeIn(1, 16),
    step: 1,
  },
  {
    name: "previewScale",
    label: m.system.previewScale,
    hint: m.system.previewScaleHint,
    check: numberIn(0.25, 1),
    step: 0.05,
  },
  {
    name: "finalScale",
    label: m.system.finalScale,
    hint: m.system.finalScaleHint,
    check: numberIn(0.25, 2),
    step: 0.05,
  },
] as const

function ProfileCard({ profile, current }: { profile: HostProfile; current: boolean }) {
  const save = useSaveProfile()
  const form = useForm({
    defaultValues: toValues(profile),
    onSubmit: async ({ value }) => {
      try {
        await save.mutateAsync(toProfile(profile.name, value, profile))
        toast.success(m.system.saved(profile.name))
      } catch {
        // shown in the banner
      }
    },
  })
  const submitted = () => form.state.submissionAttempts > 0
  const id = (field: string) => `profile-${profile.name}-${field}`

  return (
    <Card className={cn(current && "ring-2 ring-primary/40 dark:ring-primary/50")}>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <span className="font-mono">{profile.name}</span>
          {current ? <Badge>{m.system.current}</Badge> : null}
          {profile.paused ? <Badge variant="outline">{m.system.paused}</Badge> : null}
        </CardTitle>
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
          <FieldSet className="gap-3">
            <FieldLegend variant="label" className="mb-0">
              {m.system.concurrency}
            </FieldLegend>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
              {QUEUES.map((q) => (
                <form.Field
                  key={q}
                  name={`concurrency.${q}`}
                  validators={{
                    onChange: ({ value }) => queueError(value),
                    onSubmit: ({ value }) => queueError(value),
                  }}
                >
                  {(field) => {
                    const error = shownError(field.state.meta, submitted())
                    return (
                      <Field data-invalid={!!error || undefined} className="gap-1.5">
                        <FieldLabel htmlFor={id(q)} className="text-xs text-muted-foreground">
                          {m.system.queues[q]}
                        </FieldLabel>
                        <Input
                          id={id(q)}
                          type="number"
                          inputMode="numeric"
                          min={0}
                          max={8}
                          step={1}
                          className="tabular"
                          value={field.state.value}
                          onChange={(e) => field.handleChange(e.target.value)}
                          onBlur={field.handleBlur}
                          aria-invalid={!!error || undefined}
                        />
                        {error ? <FieldError>{error}</FieldError> : null}
                      </Field>
                    )
                  }}
                </form.Field>
              ))}
            </div>
            <FieldDescription>{m.system.concurrencyHint}</FieldDescription>
          </FieldSet>

          <div className="grid gap-4 sm:grid-cols-2">
            {(
              [
                ["heavyExclusive", m.system.heavyExclusive, m.system.heavyExclusiveHint],
                ["paused", m.system.paused, m.system.pausedHint],
              ] as const
            ).map(([name, label, hint]) => (
              <form.Field key={name} name={name}>
                {(field) => (
                  <Field orientation="horizontal" className="rounded-3xl border px-4 py-3">
                    <FieldContent>
                      <FieldLabel htmlFor={id(name)}>{label}</FieldLabel>
                      <FieldDescription>{hint}</FieldDescription>
                    </FieldContent>
                    <Switch
                      id={id(name)}
                      checked={field.state.value}
                      onCheckedChange={(next) => field.handleChange(next)}
                    />
                  </Field>
                )}
              </form.Field>
            ))}
          </div>

          <div className="grid gap-5 sm:grid-cols-2">
            {NUMBER_FIELDS.map((f) => (
              <form.Field
                key={f.name}
                name={f.name}
                validators={{
                  onChange: ({ value }) => f.check(value),
                  onSubmit: ({ value }) => f.check(value),
                }}
              >
                {(field) => {
                  const error = shownError(field.state.meta, submitted())
                  return (
                    <Row label={f.label} htmlFor={id(f.name)} hint={f.hint} error={error}>
                      <Input
                        id={id(f.name)}
                        type="number"
                        inputMode={f.step === 1 ? "numeric" : "decimal"}
                        step={f.step}
                        className="max-w-36 tabular"
                        value={field.state.value}
                        onChange={(e) => field.handleChange(e.target.value)}
                        onBlur={field.handleBlur}
                        aria-invalid={!!error || undefined}
                      />
                    </Row>
                  )
                }}
              </form.Field>
            ))}
          </div>

          {save.error ? <Banner title={errorMessage(save.error)} /> : null}
          <div className="flex justify-end">
            <form.Subscribe selector={(s) => [s.isSubmitting, s.isDirty] as const}>
              {([submitting, dirty]) => (
                <Button type="submit" disabled={submitting || !dirty}>
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

function NewProfileDialog({
  open,
  onOpenChange,
  template,
  taken,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  template: HostProfile
  taken: string[]
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        {open ? (
          <NewProfileForm template={template} taken={taken} onDone={() => onOpenChange(false)} />
        ) : null}
      </DialogContent>
    </Dialog>
  )
}

function NewProfileForm({
  template,
  taken,
  onDone,
}: {
  template: HostProfile
  taken: string[]
  onDone: () => void
}) {
  const save = useSaveProfile()
  const [name, setName] = useState("")
  const [touched, setTouched] = useState(false)
  const trimmed = name.trim()
  let error: string | undefined
  if (!PROFILE_NAME.test(trimmed)) error = m.system.nameInvalid
  else if (taken.includes(trimmed)) error = m.system.nameTaken
  const shown = touched ? error : undefined

  const submit = async () => {
    setTouched(true)
    if (error) return
    try {
      await save.mutateAsync({ ...template, name: trimmed })
      toast.success(m.system.created(trimmed))
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
        <DialogTitle>{m.system.newProfileTitle}</DialogTitle>
        <DialogDescription>{m.system.newProfileDescription(template.name)}</DialogDescription>
      </DialogHeader>
      <Field data-invalid={!!shown || undefined}>
        <FieldLabel htmlFor="new-profile-name">{m.system.name}</FieldLabel>
        <Input
          id="new-profile-name"
          autoComplete="off"
          spellCheck={false}
          maxLength={32}
          placeholder="mac-mini"
          className="font-mono"
          value={name}
          onChange={(e) => setName(e.target.value.toLowerCase())}
          onBlur={() => setTouched(true)}
          aria-invalid={!!shown || undefined}
        />
        {shown ? (
          <FieldError>{shown}</FieldError>
        ) : (
          <FieldDescription>{m.system.nameHint}</FieldDescription>
        )}
      </Field>
      {save.error ? <Banner title={errorMessage(save.error)} /> : null}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onDone}>
          {m.common.cancel}
        </Button>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? m.common.creating : m.common.create}
        </Button>
      </DialogFooter>
    </form>
  )
}
