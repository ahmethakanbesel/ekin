import { useState, type ReactNode } from "react"
import { useForm } from "@tanstack/react-form"
import { useQuery } from "@tanstack/react-query"
import { MinusIcon, PlusIcon } from "lucide-react"
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
import { Slider } from "@/components/ui/slider"
import { Switch } from "@/components/ui/switch"
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group"
import { QueueDot } from "@/features/jobs/queue"
import { Row, shownError } from "@/features/pipelines/form-parts"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { profilesQuery, useSaveProfile } from "@/queries/system"

export const QUEUES = [
  "control",
  "agent",
  "tts",
  "render",
  "extract",
  "io",
] as const
type Queue = (typeof QUEUES)[number]
/** What heavy exclusivity covers when a profile does not say. */
const DEFAULT_HEAVY: Queue[] = ["agent", "tts", "render"]

export function heavyOf(p: HostProfile): Queue[] {
  return p.heavyQueues ?? DEFAULT_HEAVY
}

/** Named job priorities, as nice values. */
export const PRIORITIES = [
  { nice: 0, label: m.system.priorities.normal },
  { nice: 10, label: m.system.priorities.low },
  { nice: 19, label: m.system.priorities.lowest },
] as const

/** Toggles for the queues heavy exclusivity covers, in queue order. */
export function HeavyQueuesPicker({
  value,
  onChange,
}: {
  value: Queue[]
  onChange: (next: Queue[]) => void
}) {
  return (
    <ToggleGroup
      multiple
      variant="outline"
      size="sm"
      spacing={1.5}
      aria-label={m.system.heavyQueues}
      value={value}
      onValueChange={(next) => onChange(QUEUES.filter((q) => next.includes(q)))}
      className="flex-wrap"
    >
      {/* Off queues fade, so the pressed fill is not the only cue. */}
      {QUEUES.map((q) => (
        <ToggleGroupItem
          key={q}
          value={q}
          className="text-muted-foreground aria-pressed:text-foreground"
        >
          <QueueDot
            queue={q}
            className={cn(!value.includes(q) && "opacity-40")}
          />
          {m.system.queues[q]}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

const PREVIEW_FPS = ["15", "24", "30"] as const
const FINAL_FPS = ["30", "60"] as const

/** One frame rate out of a few, as pressed buttons. */
function FpsPicker({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: readonly string[]
  value: string
  onChange: (next: string) => void
}) {
  return (
    <ToggleGroup
      variant="outline"
      size="sm"
      spacing={1.5}
      aria-label={label}
      value={[value]}
      onValueChange={(next) => next[0] && onChange(String(next[0]))}
    >
      {options.map((fps) => (
        <ToggleGroupItem
          key={fps}
          value={fps}
          className="tabular text-muted-foreground aria-pressed:text-foreground"
        >
          {m.system.fps(fps)}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

const PROFILE_NAME = /^[a-z0-9][a-z0-9-]{0,31}$/

/** Admins: how hard the workers using each profile may work. */
export function HostProfiles() {
  const query = useQuery(profilesQuery())
  const [creating, setCreating] = useState(false)

  let body
  if (query.isPending) body = <Skeleton className="h-96 w-full rounded-xl" />
  else if (query.isError)
    body = (
      <ErrorState error={query.error} onRetry={() => void query.refetch()} />
    )
  else {
    const { profiles, current } = query.data
    // The server's own profile first, the rest by name.
    const ordered = profiles.toSorted((a, b) =>
      a.name === current
        ? -1
        : b.name === current
          ? 1
          : a.name.localeCompare(b.name)
    )
    body = (
      <div className="grid gap-6">
        {ordered.map((p) => (
          // Re-mount the form when the stored profile changes underneath it.
          <ProfileCard
            key={JSON.stringify(p)}
            profile={p}
            current={p.name === current}
          />
        ))}
      </div>
    )
  }

  const template =
    query.data?.profiles.find((p) => p.name === query.data.current) ??
    query.data?.profiles[0]

  return (
    <section id="profiles" className="scroll-mt-8">
      <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
        <div className="space-y-1">
          <h2 className="font-heading text-lg font-semibold tracking-tight">
            {m.system.profilesTitle}
          </h2>
          <p className="text-sm text-muted-foreground">
            {m.system.profilesDescription}
          </p>
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
  heavyQueues: Queue[]
  paused: boolean
  cooldownSeconds: string
  cpuLimit: string
  nice: string
  renderDuty: string
  renderChunks: string
  previewScale: string
  finalScale: string
  previewFps: string
  finalFps: string
}

function toValues(p: HostProfile): Values {
  const concurrency: Record<string, string> = {}
  for (const q of QUEUES) concurrency[q] = String(p.concurrency[q] ?? 0)
  return {
    concurrency,
    heavyExclusive: p.heavyExclusive,
    heavyQueues: heavyOf(p),
    paused: p.paused,
    cooldownSeconds: String(p.cooldownSeconds),
    cpuLimit: String(p.cpuLimit),
    nice: String(p.nice ?? 0),
    renderDuty: String(p.renderDuty),
    renderChunks: String(p.renderChunks),
    previewScale: String(p.previewScale),
    finalScale: String(p.finalScale),
    previewFps: String(p.previewFps ?? 30),
    finalFps: String(p.finalFps ?? 30),
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
    heavyQueues: v.heavyQueues,
    paused: v.paused,
    cooldownSeconds: Number(v.cooldownSeconds),
    cpuLimit: Number(v.cpuLimit),
    nice: Number(v.nice),
    renderDuty: Number(v.renderDuty),
    renderChunks: Number(v.renderChunks),
    previewScale: Number(v.previewScale),
    finalScale: Number(v.finalScale),
    previewFps: Number(v.previewFps) as HostProfile["previewFps"],
    finalFps: Number(v.finalFps) as HostProfile["finalFps"],
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


/** 0 (no limit) or 10–100 percent. */
export function cpuLimitError(s: string): string | undefined {
  const t = s.trim()
  return t === "0" || wholeIn(10, 100)(t) === undefined
    ? undefined
    : m.system.cpuLimitRange
}

type NumberSpec = {
  name: "cooldownSeconds" | "renderDuty" | "renderChunks" | "previewScale" | "finalScale"
  label: string
  hint: string | ((value: string) => string)
  check: (s: string) => string | undefined
  step: number
}

const COOLDOWN: NumberSpec = {
  name: "cooldownSeconds",
  label: m.system.cooldown,
  hint: m.system.cooldownHint,
  check: wholeIn(0, 3600),
  step: 1,
}

/** A scale's hint names the resolution it gives a 1080p composition. */
const scaleHint = (hint: string) => (v: string) => {
  const n = Number(v)
  return Number.isFinite(n) && n > 0 ? `${hint} ${m.system.scaleHeight(Math.round(n * 1080))}` : hint
}

const RENDER_FIELDS: NumberSpec[] = [
  { name: "renderDuty", label: m.system.renderDuty, hint: m.system.renderDutyHint, check: numberIn(0.05, 1), step: 0.05 },
  { name: "renderChunks", label: m.system.renderChunks, hint: m.system.renderChunksHint, check: wholeIn(1, 16), step: 1 },
  { name: "previewScale", label: m.system.previewScale, hint: scaleHint(m.system.previewScaleHint), check: numberIn(0.25, 1), step: 0.05 },
  { name: "finalScale", label: m.system.finalScale, hint: scaleHint(m.system.finalScaleHint), check: numberIn(0.25, 2), step: 0.05 },
]

/** A titled group of a profile's settings. */
function ProfileGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-4 border-t pt-5 first:border-t-0 first:pt-0">
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  )
}

/** −/+ around a whole number. */
function Stepper({
  label,
  value,
  min,
  max,
  onChange,
}: {
  label: string
  value: number
  min: number
  max: number
  onChange: (n: number) => void
}) {
  return (
    <div className="flex items-center gap-1">
      <Button
        type="button"
        variant="outline"
        size="icon-xs"
        aria-label={m.system.oneFewer(label)}
        disabled={value <= min}
        onClick={() => onChange(value - 1)}
      >
        <MinusIcon />
      </Button>
      <span className={cn("w-7 text-center tabular", value === 0 && "text-muted-foreground")} aria-live="polite">
        {value}
      </span>
      <Button
        type="button"
        variant="outline"
        size="icon-xs"
        aria-label={m.system.oneMore(label)}
        disabled={value >= max}
        onClick={() => onChange(value + 1)}
      >
        <PlusIcon />
      </Button>
    </div>
  )
}

/** Where the CPU limit starts when switched on. */
const DEFAULT_CPU = 50

/** A switch for the CPU limit and, when on, its share. "0" is no limit. */
function CpuLimit({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  const pct = Number(value) || 0
  const on = pct > 0 && pct < 100
  return (
    <div className="grid gap-2">
      <Field orientation="horizontal">
        <FieldContent>
          <FieldLabel htmlFor={id}>{m.system.cpuLimitTitle}</FieldLabel>
          <FieldDescription>{m.system.cpuLimitHint}</FieldDescription>
        </FieldContent>
        <span className="flex items-center gap-2">
          <span className="text-sm text-muted-foreground tabular">
            {on ? m.system.cpuLimitValue(pct) : m.system.noLimit}
          </span>
          <Switch id={id} checked={on} onCheckedChange={(next) => onChange(next ? String(DEFAULT_CPU) : "0")} />
        </span>
      </Field>
      {on ? (
        <Slider
          thumbLabel={m.system.cpuLimitTitle}
          valueText={m.system.cpuLimitValue(pct)}
          min={10}
          max={100}
          step={5}
          value={pct}
          onValueChange={(v) => onChange(String(Array.isArray(v) ? v[0] : v))}
        />
      ) : null}
    </div>
  )
}

/** Job priority as named presets; another stored nice value still shows. */
function PriorityPicker({ value, onChange }: { value: number; onChange: (nice: number) => void }) {
  const known = PRIORITIES.some((p) => p.nice === value)
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
      <span className="flex flex-col">
        <span className="text-sm font-medium" id="priority-label">
          {m.system.priority}
        </span>
        <span className="text-xs text-muted-foreground">{m.system.priorityHint}</span>
      </span>
      <ToggleGroup
        variant="outline"
        size="sm"
        spacing={0}
        aria-labelledby="priority-label"
        value={[String(value)]}
        onValueChange={(next) => next[0] !== undefined && onChange(Number(next[0]))}
      >
        {PRIORITIES.map((pr) => (
          <ToggleGroupItem key={pr.nice} value={String(pr.nice)}>
            {pr.label}
          </ToggleGroupItem>
        ))}
        {known ? null : <ToggleGroupItem value={String(value)}>{m.system.niceValue(value)}</ToggleGroupItem>}
      </ToggleGroup>
    </div>
  )
}

function ProfileCard({
  profile,
  current,
}: {
  profile: HostProfile
  current: boolean
}) {
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
  const numberField = (f: NumberSpec) => (
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
          <Row
            label={f.label}
            htmlFor={id(f.name)}
            hint={typeof f.hint === "function" ? f.hint(field.state.value) : f.hint}
            error={error}
          >
            <Input
              id={id(f.name)}
              type="number"
              inputMode={Number.isInteger(f.step) ? "numeric" : "decimal"}
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
  )

  return (
    <Card className={cn(current && "ring-2 ring-foreground/20")}>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <span className="font-mono">{profile.name}</span>
          {current ? (
            <Badge variant="secondary">{m.system.current}</Badge>
          ) : null}
          {profile.paused ? (
            <Badge variant="outline">{m.system.paused}</Badge>
          ) : null}
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
          <ProfileGroup title={m.system.groups.jobs}>
            <form.Field name="paused">
              {(field) => (
                <Field orientation="horizontal" className="rounded-xl border px-4 py-3">
                  <FieldContent>
                    <FieldLabel htmlFor={id("paused")}>{m.system.running}</FieldLabel>
                    <FieldDescription>
                      {field.state.value ? m.system.pausedHint : m.system.runningHint}
                    </FieldDescription>
                  </FieldContent>
                  <Switch
                    id={id("paused")}
                    aria-label={m.system.takeJobs}
                    checked={!field.state.value}
                    onCheckedChange={(on) => field.handleChange(!on)}
                  />
                </Field>
              )}
            </form.Field>

            <FieldSet className="gap-2">
              <FieldLegend variant="label" className="mb-0">
                {m.system.concurrency}
              </FieldLegend>
              <div className="grid gap-x-8 gap-y-1.5 sm:grid-cols-2">
                {QUEUES.map((q) => (
                  <form.Field key={q} name={`concurrency.${q}`}>
                    {(field) => (
                      <div className="flex h-8 items-center justify-between gap-3">
                        <span className="flex items-center gap-2 text-sm">
                          <QueueDot queue={q} />
                          {m.system.queues[q]}
                        </span>
                        <Stepper
                          label={m.system.queues[q]}
                          value={Number(field.state.value) || 0}
                          min={0}
                          max={8}
                          onChange={(n) => field.handleChange(String(n))}
                        />
                      </div>
                    )}
                  </form.Field>
                ))}
              </div>
              <FieldDescription>{m.system.concurrencyHint}</FieldDescription>
            </FieldSet>

            <form.Field name="heavyExclusive">
              {(field) => (
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldLabel htmlFor={id("heavyExclusive")}>{m.system.heavyExclusive}</FieldLabel>
                    <FieldDescription>{m.system.heavyExclusiveHint}</FieldDescription>
                  </FieldContent>
                  <Switch
                    id={id("heavyExclusive")}
                    checked={field.state.value}
                    onCheckedChange={(next) => field.handleChange(next)}
                  />
                </Field>
              )}
            </form.Field>
            <form.Subscribe selector={(st) => st.values.heavyExclusive}>
              {(exclusive) =>
                exclusive ? (
                  <div className="grid gap-5 sm:grid-cols-[minmax(0,1fr)_12rem]">
                    <form.Field name="heavyQueues">
                      {(field) => (
                        <FieldSet className="gap-2">
                          <FieldLegend variant="label" className="mb-0">
                            {m.system.heavyQueues}
                          </FieldLegend>
                          <HeavyQueuesPicker value={field.state.value} onChange={(next) => field.handleChange(next)} />
                          <FieldDescription>{m.system.heavyQueuesHint}</FieldDescription>
                        </FieldSet>
                      )}
                    </form.Field>
                    {numberField(COOLDOWN)}
                  </div>
                ) : null
              }
            </form.Subscribe>
          </ProfileGroup>

          <ProfileGroup title={m.system.groups.host}>
            <form.Field
              name="cpuLimit"
              validators={{ onChange: ({ value }) => cpuLimitError(value) }}
            >
              {(field) => <CpuLimit id={id("cpu")} value={field.state.value} onChange={field.handleChange} />}
            </form.Field>
            <form.Field name="nice">
              {(field) => <PriorityPicker value={Number(field.state.value) || 0} onChange={(n) => field.handleChange(String(n))} />}
            </form.Field>
          </ProfileGroup>

          <ProfileGroup title={m.system.groups.render}>
            <div className="grid gap-5 sm:grid-cols-2">
              {RENDER_FIELDS.map((f) => numberField(f))}
            </div>
          <div className="grid gap-5 sm:grid-cols-2">
            {(
              [
                ["previewFps", m.system.previewFps, m.system.previewFpsHint, PREVIEW_FPS],
                ["finalFps", m.system.finalFps, m.system.finalFpsHint, FINAL_FPS],
              ] as const
            ).map(([name, label, hint, options]) => (
              <form.Field key={name} name={name}>
                {(field) => (
                  <FieldSet className="gap-2">
                    <FieldLegend variant="label" className="mb-0">
                      {label}
                    </FieldLegend>
                    <FpsPicker
                      label={label}
                      options={options}
                      value={field.state.value}
                      onChange={(next) => field.handleChange(next)}
                    />
                    <FieldDescription>{hint}</FieldDescription>
                  </FieldSet>
                )}
              </form.Field>
            ))}
          </div>

          </ProfileGroup>

          {save.error ? <Banner title={errorMessage(save.error)} /> : null}
          <div className="flex justify-end">
            <form.Subscribe
              selector={(s) => [s.isSubmitting, s.isDirty] as const}
            >
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
          <NewProfileForm
            template={template}
            taken={taken}
            onDone={() => onOpenChange(false)}
          />
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
        <DialogDescription>
          {m.system.newProfileDescription(template.name)}
        </DialogDescription>
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
