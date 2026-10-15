import { useState } from "react"
import { useQuery } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { GaugeIcon, MinusIcon, PauseIcon, PlayIcon, PlusIcon } from "lucide-react"
import { toast } from "sonner"

import type { HostProfile } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Slider } from "@/components/ui/slider"
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover"
import { Switch } from "@/components/ui/switch"
import {
  HeavyQueuesPicker,
  PRIORITIES,
  QUEUES,
  heavyOf,
} from "@/features/system/host-profiles"
import { m } from "@/i18n"
import { errorMessage } from "@/lib/errors"
import { cn } from "@/lib/utils"
import { meQuery } from "@/queries/auth"
import { profilesQuery, useSaveProfile } from "@/queries/system"

const MAX_SLOTS = 8
const MAX_COOLDOWN = 3600
const MIN_CPU = 10
/** Where the CPU limit starts when switched on. */
const DEFAULT_CPU = 50

export type Throttle = {
  profiles: HostProfile[]
  /** The profile this server's worker uses. */
  current: string
  update: (name: string, edit: (p: HostProfile) => HostProfile) => void
}

/**
 * Host profiles for admins, saved on every change. Edits show at once
 * and stay shown until their save lands (or are rolled back if it
 * fails); the server applies a save immediately.
 */
export function useThrottle(): Throttle | undefined {
  const me = useQuery(meQuery())
  const query = useQuery({
    ...profilesQuery(),
    enabled: !!me.data?.isAdmin,
    refetchInterval: 15_000,
  })
  const save = useSaveProfile()
  const [pending, setPending] = useState<Record<string, HostProfile>>({})
  if (!query.data) return undefined

  const profiles = query.data.profiles.map((p) => pending[p.name] ?? p)
  const forget = (name: string, next: HostProfile) =>
    setPending((s) => {
      if (s[name] !== next) return s // a newer edit is on its way
      const rest = { ...s }
      delete rest[name]
      return rest
    })
  const update = (name: string, edit: (p: HostProfile) => HostProfile) => {
    const base = profiles.find((p) => p.name === name)
    if (!base) return
    const next = edit(base)
    setPending((s) => ({ ...s, [name]: next }))
    save.mutate(next, {
      onSuccess: () => forget(name, next),
      onError: (err) => {
        toast.error(errorMessage(err))
        forget(name, next)
      },
    })
  }
  return { profiles, current: query.data.current, update }
}

/** One line per paused profile, with a way to resume it. */
export function PausedBanners({ throttle }: { throttle: Throttle }) {
  const paused = throttle.profiles.filter((p) => p.paused)
  if (paused.length === 0) return null
  const t = m.jobs.throttle
  return (
    <div className="mb-4 grid gap-2">
      {paused.map((p) => (
        <div
          key={p.name}
          role="status"
          className="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-3xl border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm"
        >
          <PauseIcon className="size-4 shrink-0 text-amber-700 dark:text-amber-300" />
          <span className="min-w-0 flex-1">
            {p.name === throttle.current ? t.pausedHere(p.name) : t.pausedRemote(p.name)}
          </span>
          <Button
            size="sm"
            onClick={() => throttle.update(p.name, (x) => ({ ...x, paused: false }))}
          >
            <PlayIcon data-icon="inline-start" />
            {t.resume}
          </Button>
        </div>
      ))}
    </div>
  )
}

/** The Jobs page's quick throttle: pause, slots per queue, heavy exclusivity, cooldown. */
export function ThrottleControl({ throttle }: { throttle: Throttle }) {
  const [chosen, setChosen] = useState<string | null>(null)
  const name = chosen ?? throttle.current
  const profile =
    throttle.profiles.find((p) => p.name === name) ??
    throttle.profiles.find((p) => p.name === throttle.current) ??
    throttle.profiles[0]
  const t = m.jobs.throttle
  if (!profile) return null
  const set = (edit: (p: HostProfile) => HostProfile) => throttle.update(profile.name, edit)
  const slots = (q: string) => profile.concurrency[q] ?? 0
  const setSlots = (q: string, n: number) =>
    set((p) => ({ ...p, concurrency: { ...p.concurrency, [q]: n } }))

  return (
    <Popover>
      <PopoverTrigger render={<Button variant="outline" size="sm" />}>
        <GaugeIcon data-icon="inline-start" />
        {t.button}
        {throttle.profiles.some((p) => p.paused) ? (
          <span className="size-2 rounded-full bg-amber-500" aria-label={t.someonePaused} />
        ) : null}
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[22rem] max-w-[calc(100vw-2rem)]">
        <PopoverHeader>
          <PopoverTitle>{t.title}</PopoverTitle>
          <PopoverDescription>{t.description}</PopoverDescription>
        </PopoverHeader>

        {throttle.profiles.length > 1 ? (
          <div className="flex flex-wrap gap-1.5" role="group" aria-label={t.profile}>
            {throttle.profiles.map((p) => (
              <Button
                key={p.name}
                size="xs"
                variant={p.name === profile.name ? "default" : "outline"}
                aria-pressed={p.name === profile.name}
                onClick={() => setChosen(p.name)}
                className="font-mono"
              >
                {p.name}
                {p.name === throttle.current ? ` · ${t.thisServer}` : ""}
              </Button>
            ))}
          </div>
        ) : null}

        <label className="flex items-center justify-between gap-3 rounded-2xl border px-3 py-2.5">
          <span className="flex flex-col">
            <span className="font-medium">{profile.paused ? t.paused : t.running}</span>
            <span className="text-xs text-muted-foreground">
              {profile.paused ? t.pausedHint : t.runningHint}
            </span>
          </span>
          <Switch
            checked={!profile.paused}
            onCheckedChange={(on) => set((p) => ({ ...p, paused: !on }))}
            aria-label={t.takeJobs}
          />
        </label>

        <fieldset className="grid gap-1.5">
          <legend className="mb-1.5 text-xs font-medium text-muted-foreground">{t.slots}</legend>
          {QUEUES.map((q) => (
            <div key={q} className="flex items-center justify-between gap-3">
              <span>{m.system.queues[q]}</span>
              <Stepper
                label={m.system.queues[q]}
                value={slots(q)}
                min={0}
                max={MAX_SLOTS}
                onChange={(n) => setSlots(q, n)}
              />
            </div>
          ))}
        </fieldset>

        <label className="flex items-center justify-between gap-3">
          <span className="flex flex-col">
            <span>{m.system.heavyExclusive}</span>
            <span className="text-xs text-muted-foreground">{m.system.heavyExclusiveHint}</span>
          </span>
          <Switch
            checked={profile.heavyExclusive}
            onCheckedChange={(on) => set((p) => ({ ...p, heavyExclusive: on }))}
          />
        </label>
        {profile.heavyExclusive ? (
          <div className="grid gap-1.5">
            <span className="text-xs text-muted-foreground">{t.heavyQueues}</span>
            <HeavyQueuesPicker
              value={heavyOf(profile)}
              onChange={(next) => set((p) => ({ ...p, heavyQueues: next }))}
            />
          </div>
        ) : null}

        <CpuLimit
          key={`cpu:${profile.name}:${profile.cpuLimit}`}
          percent={profile.cpuLimit}
          onChange={(pct) => set((p) => ({ ...p, cpuLimit: pct }))}
        />

        <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
          <span className="flex flex-col">
            <span>{t.priority}</span>
            <span className="text-xs text-muted-foreground">{t.priorityHint}</span>
          </span>
          <div role="group" aria-label={t.priority} className="flex gap-1">
            {PRIORITIES.map((pr) => (
              <Button
                key={pr.nice}
                size="xs"
                variant={(profile.nice ?? 0) === pr.nice ? "default" : "outline"}
                aria-pressed={(profile.nice ?? 0) === pr.nice}
                onClick={() => set((p) => ({ ...p, nice: pr.nice }))}
              >
                {pr.label}
              </Button>
            ))}
          </div>
        </div>

        <Cooldown
          key={`cooldown:${profile.name}:${profile.cooldownSeconds}`}
          seconds={profile.cooldownSeconds}
          disabled={!profile.heavyExclusive}
          onChange={(s) => set((p) => ({ ...p, cooldownSeconds: s }))}
        />

        <Link
          to="/system"
          className="text-xs text-muted-foreground underline-offset-4 hover:underline"
        >
          {t.more}
        </Link>
      </PopoverContent>
    </Popover>
  )
}

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
  const t = m.jobs.throttle
  return (
    <div className="flex items-center gap-1">
      <Button
        variant="outline"
        size="icon-xs"
        aria-label={t.oneFewer(label)}
        disabled={value <= min}
        onClick={() => onChange(value - 1)}
      >
        <MinusIcon />
      </Button>
      <span
        className={cn("tabular w-7 text-center", value === 0 && "text-muted-foreground")}
        aria-live="polite"
      >
        {value}
      </span>
      <Button
        variant="outline"
        size="icon-xs"
        aria-label={t.oneMore(label)}
        disabled={value >= max}
        onClick={() => onChange(value + 1)}
      >
        <PlusIcon />
      </Button>
    </div>
  )
}

/** Cooldown seconds, saved on Enter or when the field loses focus. */
function Cooldown({
  seconds,
  disabled,
  onChange,
}: {
  seconds: number
  disabled: boolean
  onChange: (s: number) => void
}) {
  const [text, setText] = useState(String(seconds))
  const t = m.jobs.throttle
  const parsed = /^\d+$/.test(text.trim()) ? Number(text.trim()) : NaN
  const valid = parsed >= 0 && parsed <= MAX_COOLDOWN
  const commit = () => {
    if (valid && parsed !== seconds) onChange(parsed)
    else if (!valid) setText(String(seconds))
  }
  return (
    <label className="flex items-center justify-between gap-3">
      <span className="flex flex-col">
        <span>{t.cooldown}</span>
        <span className="text-xs text-muted-foreground">{t.cooldownHint}</span>
      </span>
      <Input
        type="number"
        inputMode="numeric"
        min={0}
        max={MAX_COOLDOWN}
        step={1}
        className="tabular h-8 w-20"
        value={text}
        disabled={disabled}
        aria-invalid={!valid || undefined}
        onChange={(e) => setText(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit()
        }}
      />
    </label>
  )
}

/** A switch for the CPU limit and, when on, its share (saved on release). */
function CpuLimit({ percent, onChange }: { percent: number; onChange: (pct: number) => void }) {
  const t = m.jobs.throttle
  const on = percent > 0 && percent < 100
  const [drag, setDrag] = useState(on ? percent : DEFAULT_CPU)
  return (
    <div className="grid gap-2">
      <label className="flex items-center justify-between gap-3">
        <span className="flex flex-col">
          <span>{t.cpuLimit}</span>
          <span className="text-xs text-muted-foreground">{t.cpuLimitHint}</span>
        </span>
        <span className="flex items-center gap-2">
          <span className="tabular text-sm text-muted-foreground">
            {on ? t.cpuLimitValue(drag) : t.noLimit}
          </span>
          <Switch checked={on} onCheckedChange={(next) => onChange(next ? drag : 0)} />
        </span>
      </label>
      {on ? (
        <Slider
          thumbLabel={t.cpuLimitAria}
          valueText={t.cpuLimitValue(drag)}
          min={MIN_CPU}
          max={100}
          step={5}
          value={drag}
          onValueChange={(v) => setDrag(Array.isArray(v) ? v[0] : v)}
          onValueCommitted={(v) => onChange(Array.isArray(v) ? v[0] : v)}
        />
      ) : null}
    </div>
  )
}
